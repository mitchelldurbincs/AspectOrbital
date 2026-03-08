package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"personal-infrastructure/pkg/hubnotify"
	applog "personal-infrastructure/pkg/logger"
)

const summaryRunTimeout = 30 * time.Second

func main() {
	logger := applog.New("finance-spoke")

	for _, envFile := range []string{"cmd/finance-spoke/.env", ".env"} {
		if err := godotenv.Load(envFile); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.Printf("unable to load %s: %v", envFile, err)
			}
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	location, err := time.LoadLocation(cfg.SummaryTimezone)
	if err != nil {
		logger.Fatalf("invalid summary timezone %q: %v", cfg.SummaryTimezone, err)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	hub := hubnotify.NewClient(cfg.HubNotifyURL, httpClient)
	plaid := newPlaidClient(cfg, httpClient)
	stateStore, err := newStateStore(cfg.StateFilePath)
	if err != nil {
		logger.Fatalf("failed to initialize state store: %v", err)
	}

	summaryScheduler := newScheduler(cfg, logger, hub, plaid, stateStore, location)
	app := &financeApp{
		cfg:       cfg,
		log:       logger,
		scheduler: summaryScheduler,
		state:     stateStore,
		plaid:     plaid,
		location:  location,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.handleHealthz)
	mux.HandleFunc("/status", app.handleStatus)
	mux.HandleFunc("/run/weekly-summary", app.handleRunWeeklySummary)
	mux.HandleFunc("/plaid/setup", app.handlePlaidSetupPage)
	mux.HandleFunc("/plaid/link-token", app.handleCreateLinkToken)
	mux.HandleFunc("/plaid/exchange-public-token", app.handleExchangePublicToken)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErrCh := make(chan error, 1)
	go func() {
		logger.Printf("finance control API listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
			return
		}
		httpErrCh <- nil
	}()

	if cfg.SummaryEnabled {
		if err := app.runDueNow(); err != nil {
			logger.Printf("initial summary check failed: %v", err)
		}
	} else {
		logger.Println("weekly summary is disabled (FINANCE_SUMMARY_ENABLED=false)")
	}

	ticker := time.NewTicker(cfg.SummaryPollInterval)
	defer ticker.Stop()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	logger.Printf("finance-spoke running (next summary at %s)", app.scheduler.nextScheduleAfter(time.Now()).In(location).Format(time.RFC3339))

	running := true
	for running {
		select {
		case <-ticker.C:
			if err := app.runDueNow(); err != nil {
				logger.Printf("summary check failed: %v", err)
			}
		case sig := <-signalCh:
			logger.Printf("received signal: %s", sig)
			running = false
		case err := <-httpErrCh:
			if err != nil {
				logger.Fatalf("finance control API failed: %v", err)
			}
			running = false
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("finance control API shutdown error: %v", err)
	}

	logger.Println("finance-spoke stopped")
}

type financeApp struct {
	cfg       config
	log       *log.Logger
	scheduler *scheduler
	state     *stateStore
	plaid     *plaidClient
	location  *time.Location
}

func (a *financeApp) runDueNow() error {
	ctx, cancel := context.WithTimeout(context.Background(), summaryRunTimeout)
	defer cancel()

	return a.scheduler.RunDue(ctx, time.Now())
}

func (a *financeApp) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *financeApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	now := time.Now()
	nextSchedule := a.scheduler.nextScheduleAfter(now)
	state := a.state.Snapshot()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                "ok",
		"summaryEnabled":        a.cfg.SummaryEnabled,
		"notifyChannel":         a.cfg.NotifyTargetChannel,
		"timezone":              a.cfg.SummaryTimezone,
		"nextScheduledAt":       nextSchedule.In(a.location),
		"plaidAccessTokenCount": len(a.cfg.PlaidAccessTokens),
		"state":                 state,
	})
}

func (a *financeApp) handleRunWeeklySummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), summaryRunTimeout)
	defer cancel()

	now := time.Now()
	if err := a.scheduler.RunNow(ctx, now); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":       "ok",
		"ranAt":        now.UTC(),
		"weekKey":      weekKeyForSchedule(a.scheduler.latestScheduleAtOrBefore(now)),
		"nextSchedule": a.scheduler.nextScheduleAfter(now).In(a.location),
	})
}

func (a *financeApp) handleCreateLinkToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.plaid.HasCredentials() {
		http.Error(w, "Plaid credentials are not configured", http.StatusBadRequest)
		return
	}

	var payload createLinkTokenRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), summaryRunTimeout)
	defer cancel()

	response, err := a.plaid.CreateLinkToken(ctx, payload.ClientUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"linkToken":  response.LinkToken,
		"expiration": response.Expiration,
		"requestId":  response.RequestID,
	})
}

func (a *financeApp) handlePlaidSetupPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, plaidSetupPage)
}

func (a *financeApp) handleExchangePublicToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.plaid.HasCredentials() {
		http.Error(w, "Plaid credentials are not configured", http.StatusBadRequest)
		return
	}

	var payload exchangePublicTokenRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	publicToken := strings.TrimSpace(payload.PublicToken)
	if publicToken == "" {
		http.Error(w, "publicToken is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), summaryRunTimeout)
	defer cancel()

	response, err := a.plaid.ExchangePublicToken(ctx, publicToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"accessToken": response.AccessToken,
		"itemId":      response.ItemID,
		"requestId":   response.RequestID,
	})
}

type createLinkTokenRequest struct {
	ClientUserID string `json:"clientUserId"`
}

type exchangePublicTokenRequest struct {
	PublicToken string `json:"publicToken"`
}

func decodeJSONBody(r *http.Request, out any) error {
	maxBodyBytes := int64(1 << 20)
	defer r.Body.Close()

	body := io.LimitReader(r.Body, maxBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}

	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

const plaidSetupPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Plaid Setup</title>
  <script src="https://cdn.plaid.com/link/v2/stable/link-initialize.js"></script>
  <style>
    body { font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif; max-width: 720px; margin: 2rem auto; padding: 0 1rem; }
    button { padding: 0.7rem 1rem; font-size: 1rem; }
    pre { background: #0f172a; color: #f8fafc; padding: 1rem; border-radius: 0.5rem; white-space: pre-wrap; word-break: break-word; }
  </style>
</head>
<body>
  <h1>Plaid setup helper</h1>
  <p>Use this page to connect Fifth Third and American Express, then copy returned access tokens into <code>PLAID_ACCESS_TOKENS</code> in your <code>cmd/finance-spoke/.env</code>.</p>
  <button id="launch">Connect account</button>
  <pre id="output">Ready.</pre>

  <script>
    const output = document.getElementById('output')
    const launch = document.getElementById('launch')

    const setOutput = (value) => { output.textContent = value }

    launch.addEventListener('click', async () => {
      try {
        setOutput('Creating link token...')
        const linkResp = await fetch('/plaid/link-token', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ clientUserId: 'local-finance-user' }),
        })
        const linkData = await linkResp.json()
        if (!linkResp.ok) {
          throw new Error(linkData || 'Failed to create link token')
        }

        const handler = Plaid.create({
          token: linkData.linkToken,
          onSuccess: async (publicToken, metadata) => {
            setOutput('Exchanging public token...')
            const exchangeResp = await fetch('/plaid/exchange-public-token', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ publicToken }),
            })

            const exchangeData = await exchangeResp.json()
            if (!exchangeResp.ok) {
              throw new Error(JSON.stringify(exchangeData))
            }

            setOutput([
              'Connected institution: ' + (metadata.institution ? metadata.institution.name : 'unknown'),
              'Item ID: ' + exchangeData.itemId,
              'Access Token (copy into cmd/finance-spoke/.env as PLAID_ACCESS_TOKENS):',
              exchangeData.accessToken,
            ].join('\n'))
          },
          onExit: (err) => {
            if (err) {
              setOutput('Link exited with error: ' + JSON.stringify(err))
              return
            }
            setOutput('Link closed.')
          },
        })

        handler.open()
      } catch (err) {
        setOutput('Error: ' + (err && err.message ? err.message : String(err)))
      }
    })
  </script>
</body>
</html>
`

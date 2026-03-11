package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"personal-infrastructure/pkg/appboot"
	"personal-infrastructure/pkg/httpjson"
	"personal-infrastructure/pkg/hubnotify"
	"personal-infrastructure/pkg/lifecycle"
	applog "personal-infrastructure/pkg/logger"
	"personal-infrastructure/pkg/spokecontrol"
)

const summaryRunTimeout = 30 * time.Second

const (
	commandCatalogService = "finance-spoke"
	commandNameStatus     = "finance-status"
)

func main() {
	logger := applog.New("finance-spoke")
	if err := run(logger); err != nil {
		logger.Printf("finance-spoke exiting: %v", err)
	}
}

func run(logger *log.Logger) error {
	appboot.LoadEnvFiles(logger, appboot.StandardEnvFiles("cmd/finance-spoke")...)

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	location, err := time.LoadLocation(cfg.SummaryTimezone)
	if err != nil {
		return fmt.Errorf("invalid summary timezone %q: %w", cfg.SummaryTimezone, err)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	hub := hubnotify.NewClient(cfg.HubNotifyURL, cfg.HubNotifyAuthToken, httpClient)
	plaid := newPlaidClient(cfg, httpClient)
	stateStore, err := newStateStore(cfg.StateFilePath)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
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
	mux.HandleFunc("/control/commands", app.handleCommands)
	mux.HandleFunc("/control/command", app.handleCommand)
	mux.HandleFunc("/run/weekly-summary", app.handleRunWeeklySummary)
	mux.HandleFunc("/plaid/setup", app.handlePlaidSetupPage)
	mux.HandleFunc("/plaid/link-token", app.handleCreateLinkToken)
	mux.HandleFunc("/plaid/exchange-public-token", app.handleExchangePublicToken)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if !cfg.SummaryEnabled {
		logger.Println("weekly summary is disabled (FINANCE_SUMMARY_ENABLED=false)")
	}

	logger.Printf("finance-spoke running (next summary at %s)", app.scheduler.nextScheduleAfter(time.Now()).In(location).Format(time.RFC3339))

	exitErr := lifecycle.RunHTTPService(lifecycle.HTTPServiceOptions{
		Logger:          logger,
		Server:          httpServer,
		ListenMessage:   fmt.Sprintf("finance control API listening on %s", cfg.HTTPAddr),
		TickInterval:    cfg.SummaryPollInterval,
		RunImmediately:  cfg.SummaryEnabled,
		ShutdownTimeout: 10 * time.Second,
		OnTick: func(context.Context) error {
			if err := app.runDueNow(); err != nil {
				logger.Printf("summary check failed: %v", err)
			}
			return nil
		},
	})

	logger.Println("finance-spoke stopped")
	return exitErr
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

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *financeApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httpjson.Write(w, http.StatusOK, a.statusPayload(time.Now()))
}

func (a *financeApp) handleRunWeeklySummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !spokecontrol.IsAuthorized(r, a.cfg.SpokeCommandAuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), summaryRunTimeout)
	defer cancel()

	now := time.Now()
	if err := a.scheduler.RunNow(ctx, now); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	httpjson.Write(w, http.StatusAccepted, map[string]any{
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
	if !spokecontrol.IsAuthorized(r, a.cfg.SpokeCommandAuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !a.plaid.HasCredentials() {
		http.Error(w, "Plaid credentials are not configured", http.StatusBadRequest)
		return
	}

	var payload createLinkTokenRequest
	if err := httpjson.DecodeBody(r, &payload); err != nil {
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

	httpjson.Write(w, http.StatusOK, map[string]any{
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
	if !spokecontrol.IsAuthorized(r, a.cfg.SpokeCommandAuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !a.plaid.HasCredentials() {
		http.Error(w, "Plaid credentials are not configured", http.StatusBadRequest)
		return
	}

	var payload exchangePublicTokenRequest
	if err := httpjson.DecodeBody(r, &payload); err != nil {
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

	httpjson.Write(w, http.StatusOK, map[string]any{
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

func (a *financeApp) statusPayload(now time.Time) map[string]any {
	nextSchedule := a.scheduler.nextScheduleAfter(now)
	state := a.state.Snapshot()

	return map[string]any{
		"status":                "ok",
		"summaryEnabled":        a.cfg.SummaryEnabled,
		"notifyChannel":         a.cfg.NotifyTargetChannel,
		"timezone":              a.cfg.SummaryTimezone,
		"nextScheduledAt":       nextSchedule.In(a.location),
		"plaidAccessTokenCount": len(a.cfg.PlaidAccessTokens),
		"state":                 state,
	}
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
  <p>Use this page to connect Fifth Third and American Express, then copy returned access tokens into <code>PLAID_ACCESS_TOKENS</code> in your root <code>.env</code>.</p>
  <label for="authToken" style="display:block;margin-bottom:.5rem">SPOKE_COMMAND_AUTH_TOKEN:</label>
  <input id="authToken" type="password" placeholder="paste your auth token" style="width:100%;padding:.5rem;font-size:1rem;margin-bottom:1rem;box-sizing:border-box" />
  <button id="launch">Connect account</button>
  <pre id="output">Ready.</pre>

  <script>
    const output = document.getElementById('output')
    const launch = document.getElementById('launch')
    const authInput = document.getElementById('authToken')

    const setOutput = (value) => { output.textContent = value }

    launch.addEventListener('click', async () => {
      const token = authInput.value.trim()
      if (!token) {
        setOutput('Error: enter your SPOKE_COMMAND_AUTH_TOKEN before connecting.')
        return
      }
      try {
        setOutput('Creating link token...')
        const linkResp = await fetch('/plaid/link-token', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
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
              headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
              body: JSON.stringify({ publicToken }),
            })

            const exchangeData = await exchangeResp.json()
            if (!exchangeResp.ok) {
              throw new Error(JSON.stringify(exchangeData))
            }

            setOutput([
              'Connected institution: ' + (metadata.institution ? metadata.institution.name : 'unknown'),
              'Item ID: ' + exchangeData.itemId,
              'Access Token (copy into root .env as PLAID_ACCESS_TOKENS):',
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

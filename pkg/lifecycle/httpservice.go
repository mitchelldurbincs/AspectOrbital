package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Hook func(context.Context) error

type HTTPServiceOptions struct {
	Logger          *log.Logger
	Server          *http.Server
	ListenMessage   string
	TickInterval    time.Duration
	RunImmediately  bool
	ShutdownTimeout time.Duration
	OnStart         Hook
	OnTick          Hook
	OnStop          Hook
	SignalCh        <-chan os.Signal
}

func RunHTTPService(opts HTTPServiceOptions) error {
	if opts.Server == nil {
		return fmt.Errorf("server is required")
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 10 * time.Second
	}

	errCh := make(chan error, 1)
	go func() {
		if opts.Logger != nil && opts.ListenMessage != "" {
			opts.Logger.Print(opts.ListenMessage)
		}
		if err := opts.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	signalCh, stopSignals := serviceSignalChannel(opts.SignalCh)
	defer stopSignals()

	serviceCtx, cancelService := context.WithCancel(context.Background())
	defer cancelService()

	if opts.OnStart != nil {
		if err := opts.OnStart(serviceCtx); err != nil {
			return err
		}
	}

	var tickCh <-chan time.Time
	var ticker *time.Ticker
	if opts.OnTick != nil && opts.TickInterval > 0 {
		ticker = time.NewTicker(opts.TickInterval)
		defer ticker.Stop()
		tickCh = ticker.C
	}

	if opts.RunImmediately && opts.OnTick != nil {
		if err := opts.OnTick(serviceCtx); err != nil {
			return err
		}
	}

	exitErr := WaitForExit(signalCh, errCh, tickCh, func() {
		if opts.OnTick == nil {
			return
		}
		if err := opts.OnTick(serviceCtx); err != nil && opts.Logger != nil {
			opts.Logger.Printf("background tick failed: %v", err)
		}
	})
	cancelService()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownTimeout)
	defer cancel()

	if opts.OnStop != nil {
		if err := opts.OnStop(shutdownCtx); err != nil && opts.Logger != nil {
			opts.Logger.Printf("stop hook failed: %v", err)
		}
	}

	if err := opts.Server.Shutdown(shutdownCtx); err != nil && opts.Logger != nil {
		opts.Logger.Printf("http server shutdown error: %v", err)
	}

	return exitErr
}

func serviceSignalChannel(provided <-chan os.Signal) (<-chan os.Signal, func()) {
	if provided != nil {
		return provided, func() {}
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	return signalCh, func() {
		signal.Stop(signalCh)
	}
}

package lifecycle

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestWaitForExitReturnsServerError(t *testing.T) {
	t.Parallel()

	sigCh := make(chan os.Signal)
	serverErrCh := make(chan error, 1)
	tickCh := make(chan time.Time)

	serverErrCh <- errors.New("boom")

	err := WaitForExit(sigCh, serverErrCh, tickCh, func() {})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestWaitForExitReturnsSignalError(t *testing.T) {
	t.Parallel()

	sigCh := make(chan os.Signal, 1)
	serverErrCh := make(chan error)
	tickCh := make(chan time.Time)

	sigCh <- syscall.SIGTERM
	err := WaitForExit(sigCh, serverErrCh, tickCh, func() {})
	if err == nil || err.Error() != "received signal: terminated" {
		t.Fatalf("unexpected signal error: %v", err)
	}
}

func TestWaitForExitCallsOnTick(t *testing.T) {
	t.Parallel()

	sigCh := make(chan os.Signal, 1)
	serverErrCh := make(chan error)
	tickCh := make(chan time.Time, 1)

	ticks := 0
	errCh := make(chan error, 1)
	go func() {
		errCh <- WaitForExit(sigCh, serverErrCh, tickCh, func() { ticks++ })
	}()

	tickCh <- time.Now()
	time.Sleep(10 * time.Millisecond)
	sigCh <- syscall.SIGTERM

	err := <-errCh
	if err == nil {
		t.Fatalf("expected signal error")
	}
	if ticks == 0 {
		t.Fatalf("expected onTick callback to be called")
	}
}

func TestWaitForExitReturnsNilWhenServerClosesCleanly(t *testing.T) {
	t.Parallel()

	sigCh := make(chan os.Signal)
	serverErrCh := make(chan error, 1)
	tickCh := make(chan time.Time)

	serverErrCh <- nil
	err := WaitForExit(sigCh, serverErrCh, tickCh, func() {})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunHTTPServiceRunsStartImmediateTickAndStop(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	sigCh := make(chan os.Signal, 1)
	var startCalls atomic.Int32
	var tickCalls atomic.Int32
	var stopCalls atomic.Int32

	go func() {
		time.Sleep(30 * time.Millisecond)
		sigCh <- syscall.SIGTERM
	}()

	err := RunHTTPService(HTTPServiceOptions{
		Logger:         log.New(&strings.Builder{}, "", 0),
		Server:         server,
		RunImmediately: true,
		TickInterval:   10 * time.Millisecond,
		SignalCh:       sigCh,
		OnStart: func(context.Context) error {
			startCalls.Add(1)
			return nil
		},
		OnTick: func(context.Context) error {
			tickCalls.Add(1)
			return nil
		},
		OnStop: func(context.Context) error {
			stopCalls.Add(1)
			return nil
		},
	})

	if err == nil || err.Error() != "received signal: terminated" {
		t.Fatalf("unexpected error: %v", err)
	}
	if startCalls.Load() != 1 {
		t.Fatalf("expected one start call, got %d", startCalls.Load())
	}
	if tickCalls.Load() < 2 {
		t.Fatalf("expected at least two tick calls, got %d", tickCalls.Load())
	}
	if stopCalls.Load() != 1 {
		t.Fatalf("expected one stop call, got %d", stopCalls.Load())
	}
}

func TestRunHTTPServiceReturnsStartError(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	defer server.Close()

	err := RunHTTPService(HTTPServiceOptions{
		Logger: log.New(&strings.Builder{}, "", 0),
		Server: server,
		OnStart: func(context.Context) error {
			return errors.New("boom")
		},
	})

	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func newTestServer(t *testing.T) *http.Server {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = listener.Close()

	return &http.Server{
		Addr:    listener.Addr().String(),
		Handler: http.NewServeMux(),
	}
}

package lifecycle

import (
	"errors"
	"os"
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

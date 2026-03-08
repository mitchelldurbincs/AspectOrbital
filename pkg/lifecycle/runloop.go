package lifecycle

import (
	"fmt"
	"os"
	"time"
)

// WaitForExit blocks until a process signal is received or the HTTP server exits.
// It invokes onTick for each tick value from tickCh.
func WaitForExit(signalCh <-chan os.Signal, serverErrCh <-chan error, tickCh <-chan time.Time, onTick func()) error {
	for {
		select {
		case <-tickCh:
			onTick()
		case sig := <-signalCh:
			return fmt.Errorf("received signal: %s", sig)
		case err := <-serverErrCh:
			return err
		}
	}
}

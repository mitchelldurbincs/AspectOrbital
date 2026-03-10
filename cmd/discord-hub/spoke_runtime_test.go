package main

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
)

func TestStartSpokeCommandSyncReturnsImmediately(t *testing.T) {
	prevDiscover := discoverSpokeBridge
	started := make(chan struct{})
	release := make(chan struct{})
	discoverSpokeBridge = func(_ *log.Logger) (*spokebridge.Bridge, error) {
		close(started)
		<-release
		return nil, nil
	}
	t.Cleanup(func() {
		discoverSpokeBridge = prevDiscover
	})

	runtime := newBridgeRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	startSpokeCommandSync(ctx, log.New(io.Discard, "", 0), &fakeCommandRegistrar{}, "app", "guild", runtime)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("expected async spoke sync startup to return immediately")
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected background spoke sync to start")
	}

	if got := runtime.unavailableMessage(); got != spokeCommandsSyncingMessage {
		t.Fatalf("expected syncing message while background sync is running, got %q", got)
	}

	close(release)
	cancel()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if got := runtime.unavailableMessage(); got == spokeCommandsUnavailableMessage {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected runtime to exit syncing state, got %q", runtime.unavailableMessage())
}

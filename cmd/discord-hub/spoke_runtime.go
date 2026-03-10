package main

import (
	"context"
	"log"
	"sync"
	"time"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
)

const (
	spokeCommandSyncInterval = 2 * time.Minute

	spokeCommandsSyncingMessage     = "That command is still syncing. Try again in a moment."
	spokeCommandsUnavailableMessage = "That command is not available right now. Try again in a moment."
)

var discoverSpokeBridge = spokebridge.DiscoverWithStatus

type bridgeRuntime struct {
	mu             sync.RWMutex
	bridge         *spokebridge.Bridge
	lastSyncErr    error
	syncInProgress bool
	hasSyncAttempt bool
}

func newBridgeRuntime() *bridgeRuntime {
	return &bridgeRuntime{}
}

func (r *bridgeRuntime) markSyncStarted() {
	if r == nil {
		return
	}

	r.mu.Lock()
	r.syncInProgress = true
	r.mu.Unlock()
}

func (r *bridgeRuntime) storeSyncResult(bridge *spokebridge.Bridge, err error) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.hasSyncAttempt = true
	r.syncInProgress = false
	r.lastSyncErr = err
	if bridge != nil {
		r.bridge = bridge
	}
}

func (r *bridgeRuntime) currentBridge() *spokebridge.Bridge {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bridge
}

func (r *bridgeRuntime) unavailableMessage() string {
	if r == nil {
		return spokeCommandsUnavailableMessage
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.syncInProgress || !r.hasSyncAttempt {
		return spokeCommandsSyncingMessage
	}

	return spokeCommandsUnavailableMessage
}

func startSpokeCommandSync(ctx context.Context, logger *log.Logger, session commandRegistrar, appID, guildID string, runtime *bridgeRuntime) {
	if runtime == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(spokeCommandSyncInterval)
		defer ticker.Stop()

		for {
			syncSpokeCommandsOnce(logger, session, appID, guildID, runtime)

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func syncSpokeCommandsOnce(logger *log.Logger, session commandRegistrar, appID, guildID string, runtime *bridgeRuntime) {
	runtime.markSyncStarted()

	bridge, err := discoverSpokeBridge(logger)
	if err == nil && bridge != nil {
		err = upsertSpokeCommands(session, appID, guildID, bridge)
		if err == nil {
			logger.Printf("spoke commands synced successfully (%d command(s))", len(bridge.CommandNames()))
		}
	}
	if err != nil {
		logger.Printf("warning: spoke command sync failed: %v", err)
	}

	runtime.storeSyncResult(bridge, err)
}

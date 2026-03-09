package main

import (
	"os"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	for _, key := range []string{
		"ACCOUNTABILITY_SPOKE_HTTP_ADDR",
		"ACCOUNTABILITY_DB_PATH",
		"ACCOUNTABILITY_EXPIRY_POLL_INTERVAL",
		"ACCOUNTABILITY_COMMAND_COMMIT",
		"ACCOUNTABILITY_COMMAND_PROOF",
		"ACCOUNTABILITY_COMMAND_STATUS",
		"ACCOUNTABILITY_COMMAND_CANCEL",
	} {
		t.Setenv(key, "")
	}
	_ = os.Unsetenv("BEEMINDER_AUTH_TOKEN")
	_ = os.Unsetenv("BEEMINDER_USERNAME")

	cfg := loadConfig()
	if cfg.HTTPAddr != "127.0.0.1:8091" {
		t.Fatalf("default HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.CommitCommandName != "commit" || cfg.CancelCommandName != "cancel" {
		t.Fatalf("unexpected default command names: %+v", cfg)
	}
}

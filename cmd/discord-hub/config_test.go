package main

import (
	"reflect"
	"strings"
	"testing"
)

func clearHubEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"DISCORD_BOT_TOKEN",
		"DISCORD_GUILD_ID",
		"HUB_HTTP_ADDR",
		"HUB_NOTIFY_AUTH_TOKEN",
		"DISCORD_CRITICAL_MENTION",
		"DISCORD_CHANNEL_MAP",
		"SPOKE_COMMANDS_ENABLED",
		"SPOKE_COMMAND_SERVICES",
	}

	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestLoadHubConfigRequiresDiscordBotToken(t *testing.T) {
	clearHubEnv(t)

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing DISCORD_BOT_TOKEN")
	}
	if !strings.Contains(err.Error(), "DISCORD_BOT_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigAddsBotPrefixWhenMissing(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "abc123")
	t.Setenv("HUB_HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("DISCORD_GUILD_ID", "guild-1")
	t.Setenv("DISCORD_CRITICAL_MENTION", "<@123>")
	t.Setenv("DISCORD_CHANNEL_MAP", "alerts:111")
	t.Setenv("SPOKE_COMMANDS_ENABLED", "true")
	t.Setenv("SPOKE_COMMAND_SERVICES", `[{"name":"beeminder-spoke","commandsUrl":"http://127.0.0.1:8090/control/commands","executeUrl":"http://127.0.0.1:8090/control/command"}]`)

	cfg, err := loadHubConfig()
	if err != nil {
		t.Fatalf("loadHubConfig returned error: %v", err)
	}

	if cfg.DiscordToken != "Bot abc123" {
		t.Fatalf("unexpected token: %q", cfg.DiscordToken)
	}
}

func TestLoadHubConfigRequiresHTTPAddrWhenUnset(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "Bot token")

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing HUB_HTTP_ADDR")
	}
	if !strings.Contains(err.Error(), "HUB_HTTP_ADDR is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigRequiresNotifyAuthTokenWhenUnset(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "Bot token")
	t.Setenv("HUB_HTTP_ADDR", "127.0.0.1:8080")

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing HUB_NOTIFY_AUTH_TOKEN")
	}
	if !strings.Contains(err.Error(), "HUB_NOTIFY_AUTH_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildChannelMapParsesDynamicMappings(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_CHANNEL_MAP", " custom-one : 222 , malformed ,custom-two:333, mandarin-streaks:444, :oops, nope:, kalshi-alerts:555 ")

	got := buildChannelMap()
	want := map[string]string{
		"kalshi-alerts":    "555",
		"mandarin-streaks": "444",
		"custom-one":       "222",
		"custom-two":       "333",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected channel map\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildChannelMapIgnoresMalformedPairsAndEmptyValues(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_CHANNEL_MAP", "badpair, :123, empty:, ok:456")

	got := buildChannelMap()
	want := map[string]string{"ok": "456"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected channel map\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestLoadHubConfigRequiresSpokeCommandsEnabled(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("HUB_HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("DISCORD_GUILD_ID", "guild-1")
	t.Setenv("DISCORD_CRITICAL_MENTION", "<@123>")

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing SPOKE_COMMANDS_ENABLED")
	}
	if !strings.Contains(err.Error(), "SPOKE_COMMANDS_ENABLED is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigRequiresSpokeBridgeURLsWhenEnabled(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("HUB_HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("DISCORD_GUILD_ID", "guild-1")
	t.Setenv("DISCORD_CRITICAL_MENTION", "<@123>")
	t.Setenv("SPOKE_COMMANDS_ENABLED", "true")

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error when spoke bridge URLs are missing")
	}
	if !strings.Contains(err.Error(), "SPOKE_COMMAND_SERVICES is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

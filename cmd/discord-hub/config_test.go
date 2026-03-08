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
		"DISCORD_CRITICAL_MENTION",
		"DISCORD_CHANNEL_KALSHI_ALERTS",
		"DISCORD_CHANNEL_MANDARIN_STREAKS",
		"DISCORD_CHANNEL_MAP",
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

	cfg, err := loadHubConfig()
	if err != nil {
		t.Fatalf("loadHubConfig returned error: %v", err)
	}

	if cfg.DiscordToken != "Bot abc123" {
		t.Fatalf("unexpected token: %q", cfg.DiscordToken)
	}
}

func TestLoadHubConfigUsesDefaultHTTPAddrWhenUnset(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "Bot token")

	cfg, err := loadHubConfig()
	if err != nil {
		t.Fatalf("loadHubConfig returned error: %v", err)
	}

	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("expected default HTTP address %q, got %q", defaultHTTPAddr, cfg.HTTPAddr)
	}
}

func TestBuildChannelMapMergesBuiltinsAndExtras(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_CHANNEL_KALSHI_ALERTS", " 111 ")
	t.Setenv("DISCORD_CHANNEL_MANDARIN_STREAKS", "")
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

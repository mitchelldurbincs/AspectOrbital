package main

import (
	"errors"
	"os"
	"strings"
)

type hubConfig struct {
	DiscordToken    string
	GuildID         string
	HTTPAddr        string
	CriticalMention string
	ChannelMap      map[string]string
}

func loadHubConfig() (hubConfig, error) {
	token := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
	if token == "" {
		return hubConfig{}, errors.New("DISCORD_BOT_TOKEN is required")
	}
	if !strings.HasPrefix(token, "Bot ") {
		token = "Bot " + token
	}

	httpAddr := strings.TrimSpace(os.Getenv("HUB_HTTP_ADDR"))
	if httpAddr == "" {
		return hubConfig{}, errors.New("HUB_HTTP_ADDR is required")
	}

	return hubConfig{
		DiscordToken:    token,
		GuildID:         strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID")),
		HTTPAddr:        httpAddr,
		CriticalMention: strings.TrimSpace(os.Getenv("DISCORD_CRITICAL_MENTION")),
		ChannelMap:      buildChannelMap(),
	}, nil
}

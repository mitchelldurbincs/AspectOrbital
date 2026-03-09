package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type hubConfig struct {
	DiscordToken    string
	GuildID         string
	HTTPAddr        string
	NotifyAuthToken string
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

	notifyAuthToken := strings.TrimSpace(os.Getenv("HUB_NOTIFY_AUTH_TOKEN"))
	if notifyAuthToken == "" {
		return hubConfig{}, errors.New("HUB_NOTIFY_AUTH_TOKEN is required")
	}

	guildID := strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID"))
	if guildID == "" {
		return hubConfig{}, errors.New("DISCORD_GUILD_ID is required")
	}

	criticalMention := strings.TrimSpace(os.Getenv("DISCORD_CRITICAL_MENTION"))
	if criticalMention == "" {
		return hubConfig{}, errors.New("DISCORD_CRITICAL_MENTION is required")
	}

	kalshiAlertsChannelID := strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_KALSHI_ALERTS"))
	if kalshiAlertsChannelID == "" {
		return hubConfig{}, errors.New("DISCORD_CHANNEL_KALSHI_ALERTS is required")
	}

	mandarinStreaksChannelID := strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_MANDARIN_STREAKS"))
	if mandarinStreaksChannelID == "" {
		return hubConfig{}, errors.New("DISCORD_CHANNEL_MANDARIN_STREAKS is required")
	}

	spokeCommandsEnabledRaw := strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_ENABLED"))
	if spokeCommandsEnabledRaw == "" {
		return hubConfig{}, errors.New("SPOKE_COMMANDS_ENABLED is required")
	}

	spokeCommandsEnabled, err := strconv.ParseBool(spokeCommandsEnabledRaw)
	if err != nil {
		return hubConfig{}, errors.New("SPOKE_COMMANDS_ENABLED must be true or false")
	}

	if spokeCommandsEnabled {
		servicesRaw := strings.TrimSpace(os.Getenv("SPOKE_COMMAND_SERVICES"))
		if servicesRaw == "" {
			return hubConfig{}, errors.New("SPOKE_COMMAND_SERVICES is required when SPOKE_COMMANDS_ENABLED=true")
		}
	}

	return hubConfig{
		DiscordToken:    token,
		GuildID:         guildID,
		HTTPAddr:        httpAddr,
		NotifyAuthToken: notifyAuthToken,
		CriticalMention: criticalMention,
		ChannelMap:      buildChannelMap(kalshiAlertsChannelID, mandarinStreaksChannelID),
	}, nil
}

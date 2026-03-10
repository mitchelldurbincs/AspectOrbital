package main

import (
	"errors"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

type hubConfig struct {
	DiscordToken          string `envconfig:"DISCORD_BOT_TOKEN" required:"true"`
	GuildID               string `envconfig:"DISCORD_GUILD_ID" required:"true"`
	HTTPAddr              string `envconfig:"HUB_HTTP_ADDR" required:"true"`
	NotifyAuthToken       string `envconfig:"HUB_NOTIFY_AUTH_TOKEN" required:"true"`
	SpokeCommandAuthToken string `envconfig:"SPOKE_COMMAND_AUTH_TOKEN" required:"true"`
	CallbackAuthToken     string `envconfig:"HUB_CALLBACK_AUTH_TOKEN" required:"true"`
	SpokeCommandsEnabled  bool   `envconfig:"SPOKE_COMMANDS_ENABLED" required:"true"`
	SpokeCommandServices  string `envconfig:"SPOKE_COMMAND_SERVICES"`

	ChannelMap map[string]string `ignored:"true"`
}

func loadHubConfig() (hubConfig, error) {
	var cfg hubConfig
	if err := envconfig.Process("", &cfg); err != nil {
		return hubConfig{}, err
	}

	if !strings.HasPrefix(cfg.DiscordToken, "Bot ") {
		cfg.DiscordToken = "Bot " + cfg.DiscordToken
	}

	if cfg.SpokeCommandsEnabled {
		if strings.TrimSpace(cfg.SpokeCommandServices) == "" {
			return hubConfig{}, errors.New("SPOKE_COMMAND_SERVICES is required when SPOKE_COMMANDS_ENABLED=true")
		}
	}

	cfg.ChannelMap = buildChannelMap()

	return cfg, nil
}

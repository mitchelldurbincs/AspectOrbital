package main

import (
	"os"
	"strings"
)

func buildChannelMap(kalshiAlertsChannelID, mandarinStreaksChannelID string) map[string]string {
	channelMap := map[string]string{
		"kalshi-alerts":    strings.TrimSpace(kalshiAlertsChannelID),
		"mandarin-streaks": strings.TrimSpace(mandarinStreaksChannelID),
	}

	extras := strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_MAP"))
	if extras != "" {
		pairs := strings.Split(extras, ",")
		for _, pair := range pairs {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key == "" || value == "" {
				continue
			}

			channelMap[key] = value
		}
	}

	for key, value := range channelMap {
		if value == "" {
			delete(channelMap, key)
		}
	}

	return channelMap
}

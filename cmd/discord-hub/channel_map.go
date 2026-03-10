package main

import (
	"fmt"
	"os"
	"strings"
)

func buildChannelMap() (map[string]string, error) {
	channelMap := make(map[string]string)

	extras := strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_MAP"))
	if extras != "" {
		pairs := strings.Split(extras, ",")
		for idx, pair := range pairs {
			entry := strings.TrimSpace(pair)
			parts := strings.SplitN(entry, ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("DISCORD_CHANNEL_MAP entry %d must use name:id format", idx+1)
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key == "" || value == "" {
				return nil, fmt.Errorf("DISCORD_CHANNEL_MAP entry %d must include both channel name and channel id", idx+1)
			}

			channelMap[key] = value
		}
	}

	return channelMap, nil
}

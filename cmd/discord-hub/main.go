package main

import (
	"personal-infrastructure/pkg/appboot"
	applog "personal-infrastructure/pkg/logger"
)

func main() {
	logger := applog.New("discord-hub")

	appboot.LoadEnvFiles(logger, appboot.StandardEnvFiles("cmd/discord-hub")...)

	cfg, err := loadHubConfig()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	if err := runHub(logger, cfg); err != nil {
		logger.Fatal(err)
	}
}

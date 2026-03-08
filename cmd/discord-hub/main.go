package main

import (
	"errors"
	"os"

	"github.com/joho/godotenv"

	applog "personal-infrastructure/pkg/logger"
)

func main() {
	logger := applog.New("discord-hub")

	for _, envFile := range []string{"cmd/discord-hub/.env", ".env"} {
		if err := godotenv.Load(envFile); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.Printf("unable to load %s: %v", envFile, err)
			}
		}
	}

	cfg, err := loadHubConfig()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	if err := runHub(logger, cfg); err != nil {
		logger.Fatal(err)
	}
}

package appboot

import (
	"errors"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func StandardEnvFiles(serviceDir string) []string {
	return []string{serviceDir + "/.env", ".env"}
}

func LoadEnvFiles(logger *log.Logger, paths ...string) {
	for _, path := range paths {
		if err := godotenv.Load(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.Printf("unable to load %s: %v", path, err)
			}
		}
	}
}

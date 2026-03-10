package discordcallback

import (
	"errors"
	"strings"

	"personal-infrastructure/pkg/hubnotify"
)

func Validate(payload hubnotify.ActionCallbackRequest, expectedService string, expectedEvent string) error {
	if payload.Version != hubnotify.Version2 {
		return errors.New("version must be 2")
	}
	if strings.TrimSpace(payload.Service) != strings.TrimSpace(expectedService) {
		return errors.New("service must be " + strings.TrimSpace(expectedService))
	}
	if strings.TrimSpace(payload.Event) != strings.TrimSpace(expectedEvent) {
		return errors.New("event must be " + strings.TrimSpace(expectedEvent))
	}
	if strings.TrimSpace(payload.Context.DiscordUserID) == "" {
		return errors.New("context.discordUserId is required")
	}
	if strings.TrimSpace(payload.Action.ID) == "" {
		return errors.New("action.id is required")
	}

	return nil
}

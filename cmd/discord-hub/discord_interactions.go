package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var respondEphemeralFunc = respondEphemeral

func interactionHandler(logger *log.Logger, spokeBridge *spokeCommandBridge) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i == nil || i.Type != discordgo.InteractionApplicationCommand {
			return
		}

		commandData := i.ApplicationCommandData()

		if commandData.Name == pingCommandName {
			if err := respondEphemeralFunc(s, i.Interaction, "pong"); err != nil {
				logger.Printf("failed to respond to /ping: %v", err)
			}
			return
		}

		if spokeBridge == nil || !spokeBridge.OwnsCommand(commandData.Name) {
			if err := respondEphemeralFunc(s, i.Interaction, "That command is not available right now. Try again in a moment."); err != nil {
				logger.Printf("failed to respond to /%s: %v", commandData.Name, err)
			}
			return
		}

		options := interactionOptionValues(commandData.Options)

		execCtx, cancel := context.WithTimeout(context.Background(), spokeCommandHTTPTimeout)
		defer cancel()

		message, err := spokeBridge.ExecuteCommand(execCtx, commandData.Name, options)
		if err != nil {
			logger.Printf("spoke command %q failed: %v", commandData.Name, err)
			if respondErr := respondEphemeralFunc(s, i.Interaction, formatSpokeCommandFailure(err)); respondErr != nil {
				logger.Printf("failed to send spoke command error response: %v", respondErr)
			}
			return
		}

		if err := respondEphemeralFunc(s, i.Interaction, message); err != nil {
			logger.Printf("failed to respond to /%s: %v", commandData.Name, err)
		}
	}
}

func respondEphemeral(session *discordgo.Session, interaction *discordgo.Interaction, message string) error {
	return session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func interactionOptionValues(options []*discordgo.ApplicationCommandInteractionDataOption) map[string]any {
	if len(options) == 0 {
		return nil
	}

	values := make(map[string]any, len(options))

	for _, option := range options {
		name := strings.ToLower(strings.TrimSpace(option.Name))
		if name == "" {
			continue
		}

		switch value := option.Value.(type) {
		case string:
			values[name] = strings.TrimSpace(value)
		case bool:
			values[name] = value
		case int:
			values[name] = value
		case int64:
			values[name] = value
		case float64:
			values[name] = value
		case *discordgo.MessageAttachment:
			if value != nil {
				values[name] = map[string]any{
					"id":           strings.TrimSpace(value.ID),
					"filename":     strings.TrimSpace(value.Filename),
					"url":          strings.TrimSpace(value.URL),
					"content_type": strings.TrimSpace(value.ContentType),
					"size":         value.Size,
				}
			}
		default:
			if option.Value != nil {
				values[name] = fmt.Sprint(option.Value)
			}
		}
	}

	if len(values) == 0 {
		return nil
	}

	return values
}

package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
	"personal-infrastructure/pkg/spokecontract"
)

var respondEphemeralFunc = respondEphemeral
var deferEphemeralFunc = deferEphemeral
var followupEphemeralFunc = followupEphemeral

func interactionHandler(logger *log.Logger, runtime *bridgeRuntime) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

		spokeBridge := runtime.currentBridge()
		if spokeBridge == nil || !spokeBridge.OwnsCommand(commandData.Name) {
			if err := respondEphemeralFunc(s, i.Interaction, runtime.unavailableMessage()); err != nil {
				logger.Printf("failed to respond to /%s: %v", commandData.Name, err)
			}
			return
		}

		options := interactionOptionValues(commandData.Options)

		if err := deferEphemeralFunc(s, i.Interaction); err != nil {
			logger.Printf("failed to defer /%s response: %v", commandData.Name, err)
			return
		}

		execCtx, cancel := context.WithTimeout(context.Background(), spokebridge.CommandHTTPTimeout)
		defer cancel()

		commandContext := interactionCommandContext(i)
		message, err := spokeBridge.ExecuteCommand(execCtx, commandData.Name, commandContext, options)
		if err != nil {
			logger.Printf("spoke command %q failed: %v", commandData.Name, err)
			if respondErr := followupEphemeralFunc(s, i.Interaction, spokebridge.FormatCommandFailure(err)); respondErr != nil {
				logger.Printf("failed to send spoke command error response: %v", respondErr)
			}
			return
		}

		if err := followupEphemeralFunc(s, i.Interaction, message); err != nil {
			logger.Printf("failed to respond to /%s: %v", commandData.Name, err)
		}
	}
}

func interactionCommandContext(i *discordgo.InteractionCreate) spokecontract.CommandContext {
	ctx := spokecontract.CommandContext{
		GuildID:   strings.TrimSpace(i.GuildID),
		ChannelID: strings.TrimSpace(i.ChannelID),
	}

	if i.Member != nil && i.Member.User != nil {
		ctx.DiscordUserID = strings.TrimSpace(i.Member.User.ID)
	}
	if ctx.DiscordUserID == "" && i.User != nil {
		ctx.DiscordUserID = strings.TrimSpace(i.User.ID)
	}

	return ctx
}

func deferEphemeral(session *discordgo.Session, interaction *discordgo.Interaction) error {
	return session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
}

func followupEphemeral(session *discordgo.Session, interaction *discordgo.Interaction, message string) error {
	_, err := session.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content: message,
		Flags:   discordgo.MessageFlagsEphemeral,
	})

	return err
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
		if option == nil {
			continue
		}

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

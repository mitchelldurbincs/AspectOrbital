package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
	"personal-infrastructure/pkg/hubnotify"
	"personal-infrastructure/pkg/spokecontract"
)

var respondEphemeralFunc = respondEphemeral
var deferEphemeralFunc = deferEphemeral
var followupEphemeralFunc = followupEphemeral

type actionCallbackDispatcher interface {
	Dispatch(ctx context.Context, callbackURL string, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, error)
}

type httpActionCallbackDispatcher struct {
	httpClient *http.Client
	authToken  string
}

func newActionCallbackDispatcher(httpClient *http.Client, authToken string) actionCallbackDispatcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: spokebridge.CommandHTTPTimeout}
	}
	return &httpActionCallbackDispatcher{httpClient: httpClient, authToken: strings.TrimSpace(authToken)}
}

func (d *httpActionCallbackDispatcher) Dispatch(ctx context.Context, callbackURL string, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.authToken)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = resp.Status
		}
		return hubnotify.ActionCallbackResponse{}, errors.New(message)
	}

	var callbackResponse hubnotify.ActionCallbackResponse
	if err := json.Unmarshal(responseBody, &callbackResponse); err != nil {
		return hubnotify.ActionCallbackResponse{}, fmt.Errorf("invalid callback response: %w", err)
	}
	callbackResponse.Status = strings.TrimSpace(callbackResponse.Status)
	callbackResponse.Message = strings.TrimSpace(callbackResponse.Message)
	if callbackResponse.Status == "" {
		return hubnotify.ActionCallbackResponse{}, fmt.Errorf("invalid callback response: status is required")
	}
	if callbackResponse.Message == "" {
		return hubnotify.ActionCallbackResponse{}, fmt.Errorf("invalid callback response: message is required")
	}

	return callbackResponse, nil
}

func interactionHandler(logger *log.Logger, runtime *bridgeRuntime, callbackDispatcher actionCallbackDispatcher) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i == nil {
			return
		}
		if i.Type == discordgo.InteractionMessageComponent {
			handleMessageComponentInteraction(logger, callbackDispatcher, s, i)
			return
		}
		if i.Type != discordgo.InteractionApplicationCommand {
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

func handleMessageComponentInteraction(logger *log.Logger, callbackDispatcher actionCallbackDispatcher, session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if callbackDispatcher == nil {
		if err := respondEphemeralFunc(session, interaction.Interaction, "That action is not available right now."); err != nil {
			logger.Printf("failed to respond to component interaction: %v", err)
		}
		return
	}

	componentData := interaction.MessageComponentData()
	callbackURL, service, event, actionID, err := decodeNotifyActionCustomID(strings.TrimSpace(componentData.CustomID))
	if err != nil {
		if respondErr := respondEphemeralFunc(session, interaction.Interaction, "That action is invalid."); respondErr != nil {
			logger.Printf("failed to respond to invalid component interaction: %v", respondErr)
		}
		return
	}

	if err := deferEphemeralFunc(session, interaction.Interaction); err != nil {
		logger.Printf("failed to defer component interaction: %v", err)
		return
	}

	action, ok := findClickedAction(interaction.Message, componentData.CustomID)
	if !ok {
		if respondErr := followupEphemeralFunc(session, interaction.Interaction, "That action is no longer available."); respondErr != nil {
			logger.Printf("failed to respond to missing component action: %v", respondErr)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), spokebridge.CommandHTTPTimeout)
	defer cancel()

	response, err := callbackDispatcher.Dispatch(ctx, callbackURL, hubnotify.ActionCallbackRequest{
		Version:  hubnotify.Version2,
		Service:  service,
		Event:    event,
		Severity: inferMessageSeverity(interaction.Message),
		Action: hubnotify.ActionCallbackAction{
			ID:    actionID,
			Label: action.Label,
			Style: normalizeButtonStyle(action.Style),
		},
		Context: hubnotify.ActionCallbackContext{
			DiscordUserID: interactionCommandContext(interaction).DiscordUserID,
			GuildID:       strings.TrimSpace(interaction.GuildID),
			ChannelID:     strings.TrimSpace(interaction.ChannelID),
			MessageID:     messageID(interaction.Interaction),
			InteractionID: strings.TrimSpace(interaction.ID),
		},
		SourceTitle: sourceMessageTitle(interaction.Message),
		SourceURL:   sourceMessageURL(interaction.Message),
	})
	if err != nil {
		logger.Printf("component callback failed: %v", err)
		if respondErr := followupEphemeralFunc(session, interaction.Interaction, fmt.Sprintf("Action failed: %v", err)); respondErr != nil {
			logger.Printf("failed to respond to component callback error: %v", respondErr)
		}
		return
	}

	if err := followupEphemeralFunc(session, interaction.Interaction, response.Message); err != nil {
		logger.Printf("failed to send component callback response: %v", err)
	}
}

func findClickedAction(message *discordgo.Message, customID string) (discordgo.Button, bool) {
	if message == nil {
		return discordgo.Button{}, false
	}
	for _, component := range message.Components {
		row, ok := component.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, child := range row.Components {
			button, ok := child.(discordgo.Button)
			if ok && strings.TrimSpace(button.CustomID) == customID {
				return button, true
			}
		}
	}
	return discordgo.Button{}, false
}

func normalizeButtonStyle(style discordgo.ButtonStyle) string {
	switch style {
	case discordgo.PrimaryButton:
		return hubnotify.ActionStylePrimary
	case discordgo.SecondaryButton:
		return hubnotify.ActionStyleSecondary
	case discordgo.SuccessButton:
		return hubnotify.ActionStyleSuccess
	case discordgo.DangerButton:
		return hubnotify.ActionStyleDanger
	default:
		return "unknown"
	}
}

func inferMessageSeverity(message *discordgo.Message) string {
	if message == nil || len(message.Embeds) == 0 {
		return ""
	}
	for severity, color := range notifySeverityColors {
		if message.Embeds[0].Color == color {
			return severity
		}
	}
	return ""
}

func sourceMessageTitle(message *discordgo.Message) string {
	if message == nil || len(message.Embeds) == 0 {
		return ""
	}
	return strings.TrimSpace(message.Embeds[0].Title)
}

func sourceMessageURL(message *discordgo.Message) string {
	if message == nil || len(message.Embeds) == 0 {
		return ""
	}
	return strings.TrimSpace(message.Embeds[0].URL)
}

func messageID(interaction *discordgo.Interaction) string {
	if interaction == nil || interaction.Message == nil {
		return ""
	}
	return strings.TrimSpace(interaction.Message.ID)
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

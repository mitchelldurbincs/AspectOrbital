package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func upsertPingCommand(session *discordgo.Session, appID, guildID string) (*discordgo.ApplicationCommand, error) {
	command := &discordgo.ApplicationCommand{
		Name:        pingCommandName,
		Description: "Check whether discord-hub is alive",
	}

	return upsertCommand(session, appID, guildID, command)
}

func upsertSpokeCommands(session *discordgo.Session, appID, guildID string, bridge *spokeCommandBridge) error {
	for _, command := range bridge.BuildDiscordCommands() {
		if _, err := upsertCommand(session, appID, guildID, command); err != nil {
			return err
		}
	}

	return nil
}

func upsertCommand(session *discordgo.Session, appID, guildID string, command *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
	existingCommands, err := session.ApplicationCommands(appID, guildID)
	if err != nil {
		return nil, fmt.Errorf("could not list existing commands for /%s: %w", command.Name, err)
	}

	for _, existing := range existingCommands {
		if existing.Name == command.Name {
			edited, editErr := session.ApplicationCommandEdit(appID, guildID, existing.ID, command)
			if editErr != nil {
				return nil, fmt.Errorf("could not update existing /%s command: %w", command.Name, editErr)
			}
			return edited, nil
		}
	}

	created, err := session.ApplicationCommandCreate(appID, guildID, command)
	if err != nil {
		return nil, fmt.Errorf("could not create /%s command: %w", command.Name, err)
	}

	return created, nil
}

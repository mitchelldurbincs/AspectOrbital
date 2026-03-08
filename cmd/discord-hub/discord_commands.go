package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type commandRegistrar interface {
	ApplicationCommands(appID, guildID string, options ...discordgo.RequestOption) (cmd []*discordgo.ApplicationCommand, err error)
	ApplicationCommandEdit(appID, guildID, cmdID string, cmd *discordgo.ApplicationCommand, options ...discordgo.RequestOption) (updated *discordgo.ApplicationCommand, err error)
	ApplicationCommandCreate(appID, guildID string, cmd *discordgo.ApplicationCommand, options ...discordgo.RequestOption) (ccmd *discordgo.ApplicationCommand, err error)
}

func upsertPingCommand(session commandRegistrar, appID, guildID string) (*discordgo.ApplicationCommand, error) {
	command := &discordgo.ApplicationCommand{
		Name:        pingCommandName,
		Description: "Check whether discord-hub is alive",
	}

	return upsertCommand(session, appID, guildID, command)
}

func upsertSpokeCommands(session commandRegistrar, appID, guildID string, bridge *spokeCommandBridge) error {
	existingByName, err := listCommandsByName(session, appID, guildID)
	if err != nil {
		return fmt.Errorf("could not list existing commands for spoke sync: %w", err)
	}

	for _, command := range bridge.BuildDiscordCommands() {
		if _, err := upsertCommandWithCache(session, appID, guildID, command, existingByName); err != nil {
			return err
		}
	}

	return nil
}

func listCommandsByName(session commandRegistrar, appID, guildID string) (map[string]*discordgo.ApplicationCommand, error) {
	existingCommands, err := session.ApplicationCommands(appID, guildID)
	if err != nil {
		return nil, err
	}

	existingByName := make(map[string]*discordgo.ApplicationCommand, len(existingCommands))
	for _, existing := range existingCommands {
		existingByName[existing.Name] = existing
	}

	return existingByName, nil
}

func upsertCommand(session commandRegistrar, appID, guildID string, command *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
	existingByName, err := listCommandsByName(session, appID, guildID)
	if err != nil {
		return nil, fmt.Errorf("could not list existing commands for /%s: %w", command.Name, err)
	}

	return upsertCommandWithCache(session, appID, guildID, command, existingByName)
}

func upsertCommandWithCache(session commandRegistrar, appID, guildID string, command *discordgo.ApplicationCommand, existingByName map[string]*discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
	if existing, ok := existingByName[command.Name]; ok {
		edited, editErr := session.ApplicationCommandEdit(appID, guildID, existing.ID, command)
		if editErr != nil {
			return nil, fmt.Errorf("could not update existing /%s command: %w", command.Name, editErr)
		}

		existingByName[command.Name] = edited
		return edited, nil
	}

	created, err := session.ApplicationCommandCreate(appID, guildID, command)
	if err != nil {
		return nil, fmt.Errorf("could not create /%s command: %w", command.Name, err)
	}

	existingByName[command.Name] = created
	return created, nil
}

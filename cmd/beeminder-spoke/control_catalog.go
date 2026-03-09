package main

import "personal-infrastructure/pkg/spokecontract"

type commandCatalogResponse = spokecontract.CommandCatalog
type commandDefinition = spokecontract.CommandSpec
type commandOptionDefinition = spokecontract.CommandOptionSpec

func commandCatalogForConfig(cfg config) commandCatalogResponse {
	commands := []commandDefinition{
		{
			Name:        cfg.Commands.Status,
			Description: "Show progress and next reminder time",
		},
		{
			Name:        cfg.Commands.Snooze,
			Description: "Pause reminders for a duration",
			Options: []commandOptionDefinition{
				{
					Name:        snoozeDurationOptionName,
					Type:        snoozeDurationOptionType,
					Description: snoozeDurationOptionPrompt,
					Required:    false,
				},
			},
		},
		{
			Name:        cfg.Commands.Started,
			Description: "Pause reminders while you get started",
		},
		{
			Name:        cfg.Commands.Resume,
			Description: "Resume reminders immediately",
		},
	}

	return commandCatalogResponse{
		Version:  spokecontract.CatalogVersion,
		Service:  commandCatalogService,
		Commands: commands,
		Names:    cfg.Commands.All(),
	}
}

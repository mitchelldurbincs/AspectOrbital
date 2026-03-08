package main

type commandCatalogResponse struct {
	Version  int                 `json:"version"`
	Service  string              `json:"service"`
	Commands []commandDefinition `json:"commands"`
	Names    []string            `json:"commandNames"`
}

type commandDefinition struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Options     []commandOptionDefinition `json:"options,omitempty"`
}

type commandOptionDefinition struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

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
		Version:  commandCatalogVersion,
		Service:  commandCatalogService,
		Commands: commands,
		Names:    cfg.Commands.All(),
	}
}

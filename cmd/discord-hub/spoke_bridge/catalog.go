package spokebridge

import (
	"encoding/json"
	"errors"
)

func parseCommandCatalog(body []byte) ([]CommandSpec, error) {
	var catalog CommandCatalog
	if err := json.Unmarshal(body, &catalog); err == nil && len(catalog.Commands) > 0 {
		return catalog.Commands, nil
	}

	var legacy legacyCommandCatalog
	if err := json.Unmarshal(body, &legacy); err == nil && len(legacy.Commands) > 0 {
		return legacyCommandSpecs(legacy.Commands), nil
	}

	return nil, errors.New("spoke command catalog has no recognized commands payload")
}

func ParseCommandCatalog(body []byte) ([]CommandSpec, error) {
	return parseCommandCatalog(body)
}

func legacyCommandSpecs(commands []string) []CommandSpec {
	result := make([]CommandSpec, 0, len(commands))
	for _, command := range commands {
		result = append(result, CommandSpec{
			Name:        command,
			Description: legacyCommandDescription,
			Options: []CommandOptionSpec{
				{
					Name:        legacyArgumentOption,
					Type:        defaultCommandOptionType,
					Description: legacyArgumentOptionHelp,
					Required:    false,
				},
			},
		})
	}

	return result
}

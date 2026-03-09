package spokecontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	CatalogVersion = 1
)

var allowedOptionTypes = map[string]struct{}{
	"string":     {},
	"integer":    {},
	"number":     {},
	"boolean":    {},
	"attachment": {},
}

type CommandCatalog struct {
	Version  int           `json:"version"`
	Service  string        `json:"service"`
	Commands []CommandSpec `json:"commands"`
	Names    []string      `json:"commandNames,omitempty"`
}

type CommandSpec struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Options     []CommandOptionSpec `json:"options,omitempty"`
}

type CommandOptionSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type CommandContext struct {
	DiscordUserID string `json:"discordUserId"`
	GuildID       string `json:"guildId,omitempty"`
	ChannelID     string `json:"channelId,omitempty"`
}

type CommandRequest struct {
	Command string         `json:"command"`
	Context CommandContext `json:"context"`
	Options map[string]any `json:"options,omitempty"`
}

type CommandResponse struct {
	Status  string          `json:"status"`
	Command string          `json:"command"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NormalizeCommandName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeOptionType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "string":
		return "string"
	case "integer", "int":
		return "integer"
	case "number", "float", "float64", "double":
		return "number"
	case "boolean", "bool":
		return "boolean"
	case "attachment", "file":
		return "attachment"
	default:
		return ""
	}
}

func ValidateCommandName(value string) error {
	if len(value) == 0 || len(value) > 32 {
		return errors.New("must be 1-32 chars")
	}

	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}

		return errors.New("must use lowercase letters, numbers, _ or -")
	}

	return nil
}

func ValidateCatalog(catalog CommandCatalog) error {
	if err := ValidateCatalogSchema(catalog); err != nil {
		return err
	}

	if catalog.Version != CatalogVersion {
		return fmt.Errorf("version must be %d", CatalogVersion)
	}
	if strings.TrimSpace(catalog.Service) == "" {
		return errors.New("service is required")
	}
	if len(catalog.Commands) == 0 {
		return errors.New("commands must include at least one command")
	}

	seen := make(map[string]struct{}, len(catalog.Commands))
	for _, command := range catalog.Commands {
		name := NormalizeCommandName(command.Name)
		if err := ValidateCommandName(name); err != nil {
			return fmt.Errorf("invalid command name %q: %w", command.Name, err)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate command name %q", name)
		}
		seen[name] = struct{}{}

		if strings.TrimSpace(command.Description) == "" {
			return fmt.Errorf("command %q description is required", name)
		}

		optionSeen := make(map[string]struct{}, len(command.Options))
		for _, option := range command.Options {
			oname := NormalizeCommandName(option.Name)
			if err := ValidateCommandName(oname); err != nil {
				return fmt.Errorf("invalid option name %q for command %q: %w", option.Name, name, err)
			}
			if _, ok := optionSeen[oname]; ok {
				return fmt.Errorf("duplicate option name %q for command %q", oname, name)
			}
			optionSeen[oname] = struct{}{}

			typeName := NormalizeOptionType(option.Type)
			if typeName == "" {
				return fmt.Errorf("invalid option type %q for command %q option %q", option.Type, name, oname)
			}
			if _, ok := allowedOptionTypes[typeName]; !ok {
				return fmt.Errorf("unsupported option type %q for command %q option %q", typeName, name, oname)
			}
			if strings.TrimSpace(option.Description) == "" {
				return fmt.Errorf("description is required for command %q option %q", name, oname)
			}
		}
	}

	return nil
}

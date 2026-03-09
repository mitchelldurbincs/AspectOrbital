package spokecontract

import (
	"fmt"
	"strings"
)

func ValidateCatalogSchema(catalog CommandCatalog) error {
	if catalog.Version != CatalogVersion {
		return fmt.Errorf("version must be %d", CatalogVersion)
	}
	if strings.TrimSpace(catalog.Service) == "" {
		return fmt.Errorf("service is required")
	}
	if len(catalog.Commands) == 0 {
		return fmt.Errorf("commands must include at least one command")
	}

	for _, command := range catalog.Commands {
		name := NormalizeCommandName(command.Name)
		if err := ValidateCommandName(name); err != nil {
			return fmt.Errorf("invalid command name %q: %w", command.Name, err)
		}
		if strings.TrimSpace(command.Description) == "" {
			return fmt.Errorf("command %q description is required", name)
		}

		for _, option := range command.Options {
			oname := NormalizeCommandName(option.Name)
			if err := ValidateCommandName(oname); err != nil {
				return fmt.Errorf("invalid option name %q for command %q: %w", option.Name, name, err)
			}

			otype := NormalizeOptionType(option.Type)
			if otype == "" {
				return fmt.Errorf("invalid option type %q for command %q option %q", option.Type, name, oname)
			}
			if strings.TrimSpace(option.Description) == "" {
				return fmt.Errorf("description is required for command %q option %q", name, oname)
			}
		}
	}

	return nil
}

func ValidateCommandRequestSchema(request CommandRequest) error {
	if err := ValidateCommandName(NormalizeCommandName(request.Command)); err != nil {
		return fmt.Errorf("invalid command name %q: %w", request.Command, err)
	}
	if strings.TrimSpace(request.Context.DiscordUserID) == "" {
		return fmt.Errorf("context.discordUserId is required")
	}

	for key := range request.Options {
		if err := ValidateCommandName(NormalizeCommandName(key)); err != nil {
			return fmt.Errorf("invalid option name %q: %w", key, err)
		}
	}

	return nil
}

func ValidateCommandResponseSchema(response CommandResponse) error {
	if strings.TrimSpace(response.Status) == "" {
		return fmt.Errorf("status is required")
	}
	if err := ValidateCommandName(NormalizeCommandName(response.Command)); err != nil {
		return fmt.Errorf("invalid command name %q: %w", response.Command, err)
	}
	if strings.TrimSpace(response.Message) == "" {
		return fmt.Errorf("message is required")
	}

	return nil
}

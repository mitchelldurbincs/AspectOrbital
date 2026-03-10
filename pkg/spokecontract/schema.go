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
		name := command.Name
		if strings.TrimSpace(name) != name {
			return fmt.Errorf("command name %q must not include leading or trailing spaces", command.Name)
		}
		if strings.ToLower(name) != name {
			return fmt.Errorf("command name %q must be lowercase", command.Name)
		}
		if err := ValidateCommandName(name); err != nil {
			return fmt.Errorf("invalid command name %q: %w", command.Name, err)
		}
		if strings.TrimSpace(command.Description) == "" {
			return fmt.Errorf("command %q description is required", name)
		}

		for _, option := range command.Options {
			oname := option.Name
			if strings.TrimSpace(oname) != oname {
				return fmt.Errorf("option name %q for command %q must not include leading or trailing spaces", option.Name, name)
			}
			if strings.ToLower(oname) != oname {
				return fmt.Errorf("option name %q for command %q must be lowercase", option.Name, name)
			}
			if err := ValidateCommandName(oname); err != nil {
				return fmt.Errorf("invalid option name %q for command %q: %w", option.Name, name, err)
			}

			if err := ValidateOptionType(option.Type); err != nil {
				return fmt.Errorf("invalid option type %q for command %q option %q: %w", option.Type, name, oname, err)
			}
			if strings.TrimSpace(option.Description) == "" {
				return fmt.Errorf("description is required for command %q option %q", name, oname)
			}
		}
	}

	return nil
}

func ValidateCommandRequestSchema(request CommandRequest) error {
	if strings.TrimSpace(request.Command) != request.Command {
		return fmt.Errorf("command %q must not include leading or trailing spaces", request.Command)
	}
	if strings.ToLower(request.Command) != request.Command {
		return fmt.Errorf("command %q must be lowercase", request.Command)
	}
	if err := ValidateCommandName(request.Command); err != nil {
		return fmt.Errorf("invalid command name %q: %w", request.Command, err)
	}
	if strings.TrimSpace(request.Context.DiscordUserID) == "" {
		return fmt.Errorf("context.discordUserId is required")
	}

	for key := range request.Options {
		if strings.TrimSpace(key) != key {
			return fmt.Errorf("option name %q must not include leading or trailing spaces", key)
		}
		if strings.ToLower(key) != key {
			return fmt.Errorf("option name %q must be lowercase", key)
		}
		if err := ValidateCommandName(key); err != nil {
			return fmt.Errorf("invalid option name %q: %w", key, err)
		}
	}

	return nil
}

func ValidateCommandResponseSchema(response CommandResponse) error {
	if strings.TrimSpace(response.Status) == "" {
		return fmt.Errorf("status is required")
	}
	if strings.TrimSpace(response.Command) != response.Command {
		return fmt.Errorf("command %q must not include leading or trailing spaces", response.Command)
	}
	if strings.ToLower(response.Command) != response.Command {
		return fmt.Errorf("command %q must be lowercase", response.Command)
	}
	if err := ValidateCommandName(response.Command); err != nil {
		return fmt.Errorf("invalid command name %q: %w", response.Command, err)
	}
	if strings.TrimSpace(response.Message) == "" {
		return fmt.Errorf("message is required")
	}

	return nil
}

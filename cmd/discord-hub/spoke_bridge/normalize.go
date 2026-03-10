package spokebridge

import (
	"encoding/json"
	"fmt"
	"sort"

	"personal-infrastructure/pkg/spokecontract"
)

func NormalizeCommandSpecs(input []CommandSpec) ([]CommandSpec, error) {
	commands := make([]CommandSpec, 0, len(input))
	for _, command := range input {
		if command.Name == "ping" {
			return nil, fmt.Errorf("command %q is reserved by discord-hub", command.Name)
		}
		if err := validateOptionSpecs(command); err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands, nil
}

func validateOptionSpecs(command CommandSpec) error {
	for _, option := range command.Options {
		if !slashCommandNameRegex.MatchString(option.Name) {
			return fmt.Errorf("command %q option %q is invalid", command.Name, option.Name)
		}
		if NormalizeOptionType(option.Type) == "" {
			return fmt.Errorf("command %q option %q has unsupported type %q", command.Name, option.Name, option.Type)
		}
	}
	return nil
}

func NormalizeOptionType(raw string) string {
	return spokecontract.NormalizeOptionType(raw)
}

func (b *Bridge) CommandNames() []string {
	if b == nil {
		return nil
	}

	names := make([]string, 0, len(b.commands))
	for command := range b.commands {
		names = append(names, command)
	}
	sort.Strings(names)

	return names
}

func (b *Bridge) OwnsCommand(name string) bool {
	if b == nil {
		return false
	}

	_, ok := b.commands[name]
	return ok
}

func PruneCommandOptions(input map[string]any) (map[string]any, error) {
	if len(input) == 0 {
		return nil, nil
	}

	clean := make(map[string]any, len(input))
	for key, raw := range input {
		name := key
		if name == "" {
			return nil, fmt.Errorf("option name %q is invalid", key)
		}
		if !slashCommandNameRegex.MatchString(name) {
			return nil, fmt.Errorf("option name %q is invalid", key)
		}

		switch value := raw.(type) {
		case string:
			clean[name] = value
		case bool:
			clean[name] = value
		case map[string]any:
			clean[name] = value
		case []any:
			clean[name] = value
		case float64:
			clean[name] = value
		case int:
			clean[name] = value
		case int64:
			clean[name] = value
		case json.Number:
			clean[name] = value
		default:
			return nil, fmt.Errorf("option %q has unsupported value type %T", name, raw)
		}
	}

	if len(clean) == 0 {
		return nil, nil
	}

	return clean, nil
}

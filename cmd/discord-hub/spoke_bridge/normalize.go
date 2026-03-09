package spokebridge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"personal-infrastructure/pkg/spokecontract"
)

func NormalizeCommandSpecs(input []CommandSpec) []CommandSpec {
	seen := make(map[string]struct{}, len(input))
	commands := make([]CommandSpec, 0, len(input))

	for _, raw := range input {
		commandName := spokecontract.NormalizeCommandName(raw.Name)
		if commandName == "" || commandName == "ping" {
			continue
		}
		if !slashCommandNameRegex.MatchString(commandName) {
			continue
		}
		if _, ok := seen[commandName]; ok {
			continue
		}

		description := strings.TrimSpace(raw.Description)
		if description == "" {
			description = defaultCommandDescription
		}

		command := CommandSpec{
			Name:        commandName,
			Description: description,
			Options:     normalizeOptionSpecs(raw.Options),
		}

		seen[commandName] = struct{}{}
		commands = append(commands, command)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

func normalizeOptionSpecs(input []CommandOptionSpec) []CommandOptionSpec {
	seen := make(map[string]struct{}, len(input))
	options := make([]CommandOptionSpec, 0, len(input))

	for _, raw := range input {
		name := spokecontract.NormalizeCommandName(raw.Name)
		if name == "" {
			continue
		}
		if !slashCommandNameRegex.MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}

		optionType := NormalizeOptionType(raw.Type)
		if optionType == "" {
			continue
		}
		description := strings.TrimSpace(raw.Description)
		if description == "" {
			description = "Optional value"
		}

		options = append(options, CommandOptionSpec{
			Name:        name,
			Type:        optionType,
			Description: description,
			Required:    raw.Required,
		})
		seen[name] = struct{}{}
	}

	return options
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

	_, ok := b.commands[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func PruneCommandOptions(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}

	clean := make(map[string]any, len(input))
	for key, raw := range input {
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		if !slashCommandNameRegex.MatchString(name) {
			continue
		}

		switch value := raw.(type) {
		case string:
			clean[name] = strings.TrimSpace(value)
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
			clean[name] = fmt.Sprint(value)
		}
	}

	if len(clean) == 0 {
		return nil
	}

	return clean
}

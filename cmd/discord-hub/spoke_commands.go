package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	defaultSpokeCommandsURL        = "http://127.0.0.1:8090/control/commands"
	defaultSpokeCommandURL         = "http://127.0.0.1:8090/control/command"
	legacySpokeArgumentOption      = "argument"
	spokeDiscoveryAttemptCount     = 8
	spokeDiscoveryAttemptDelay     = 2 * time.Second
	spokeCommandHTTPTimeout        = 8 * time.Second
	legacySpokeCommandDescription  = "Command owned by beeminder-spoke"
	legacySpokeArgumentOptionHelp  = "Optional argument, like 30m"
	defaultCommandOptionTypeString = "string"
	spokeCommandFailurePrefix      = "Command failed: "
	discordResponseCharacterLimit  = 1900
)

var slashCommandNameRegex = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type spokeCommandBridge struct {
	log         *log.Logger
	httpClient  *http.Client
	commandsURL string
	commandURL  string
	commands    map[string]spokeCommandSpec
}

type spokeCommandCatalog struct {
	Version  int                `json:"version"`
	Service  string             `json:"service"`
	Commands []spokeCommandSpec `json:"commands"`
	Names    []string           `json:"commandNames,omitempty"`
}

type legacySpokeCommandCatalog struct {
	Commands []string `json:"commands"`
}

type spokeCommandSpec struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Options     []spokeCommandOptionSpec `json:"options,omitempty"`
}

type spokeCommandOptionSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type spokeCommandRequest struct {
	Command  string         `json:"command"`
	Argument string         `json:"argument,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type spokeCommandResponse struct {
	Status  string          `json:"status"`
	Command string          `json:"command"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func discoverSpokeCommandBridge(logger *log.Logger) *spokeCommandBridge {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_ENABLED")), "false") {
		logger.Println("spoke command bridge disabled by SPOKE_COMMANDS_ENABLED=false")
		return nil
	}

	commandsURL := strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_URL"))
	if commandsURL == "" {
		commandsURL = defaultSpokeCommandsURL
	}

	commandURL := strings.TrimSpace(os.Getenv("SPOKE_COMMAND_URL"))
	if commandURL == "" {
		commandURL = defaultSpokeCommandURL
	}

	bridge := &spokeCommandBridge{
		log:         logger,
		httpClient:  &http.Client{Timeout: spokeCommandHTTPTimeout},
		commandsURL: commandsURL,
		commandURL:  commandURL,
		commands:    make(map[string]spokeCommandSpec),
	}

	commands, err := bridge.fetchCommandsWithRetry()
	if err != nil {
		logger.Printf("spoke command bridge unavailable: %v", err)
		return nil
	}

	for _, command := range commands {
		bridge.commands[command.Name] = command
	}

	logger.Printf("loaded %d beeminder command(s): %s", len(commands), strings.Join(bridge.CommandNames(), ", "))
	return bridge
}

func (b *spokeCommandBridge) fetchCommandsWithRetry() ([]spokeCommandSpec, error) {
	var lastErr error

	for attempt := 1; attempt <= spokeDiscoveryAttemptCount; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), spokeCommandHTTPTimeout)
		commands, err := b.fetchCommands(ctx)
		cancel()
		if err == nil {
			return commands, nil
		}

		lastErr = err
		if attempt < spokeDiscoveryAttemptCount {
			b.log.Printf("waiting for beeminder-spoke command catalog (%d/%d): %v", attempt, spokeDiscoveryAttemptCount, err)
			time.Sleep(spokeDiscoveryAttemptDelay)
		}
	}

	return nil, lastErr
}

func (b *spokeCommandBridge) fetchCommands(ctx context.Context) ([]spokeCommandSpec, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.commandsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("spoke command catalog request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	commands, err := parseSpokeCommandCatalog(body)
	if err != nil {
		return nil, err
	}

	normalized := normalizeSpokeCommandSpecs(commands)
	if len(normalized) == 0 {
		return nil, errors.New("spoke command catalog returned no usable commands")
	}

	return normalized, nil
}

func parseSpokeCommandCatalog(body []byte) ([]spokeCommandSpec, error) {
	var catalog spokeCommandCatalog
	if err := json.Unmarshal(body, &catalog); err == nil && len(catalog.Commands) > 0 {
		return catalog.Commands, nil
	}

	var legacy legacySpokeCommandCatalog
	if err := json.Unmarshal(body, &legacy); err == nil && len(legacy.Commands) > 0 {
		return legacySpokeCommandSpecs(legacy.Commands), nil
	}

	return nil, errors.New("spoke command catalog has no recognized commands payload")
}

func legacySpokeCommandSpecs(commands []string) []spokeCommandSpec {
	result := make([]spokeCommandSpec, 0, len(commands))
	for _, command := range commands {
		result = append(result, spokeCommandSpec{
			Name:        command,
			Description: legacySpokeCommandDescription,
			Options: []spokeCommandOptionSpec{
				{
					Name:        legacySpokeArgumentOption,
					Type:        defaultCommandOptionTypeString,
					Description: legacySpokeArgumentOptionHelp,
					Required:    false,
				},
			},
		})
	}

	return result
}

func normalizeSpokeCommandSpecs(input []spokeCommandSpec) []spokeCommandSpec {
	seen := make(map[string]struct{}, len(input))
	commands := make([]spokeCommandSpec, 0, len(input))

	for _, raw := range input {
		commandName := strings.ToLower(strings.TrimSpace(raw.Name))
		if commandName == "" || commandName == pingCommandName {
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
			description = legacySpokeCommandDescription
		}

		command := spokeCommandSpec{
			Name:        commandName,
			Description: description,
			Options:     normalizeSpokeOptionSpecs(raw.Options),
		}

		seen[commandName] = struct{}{}
		commands = append(commands, command)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

func normalizeSpokeOptionSpecs(input []spokeCommandOptionSpec) []spokeCommandOptionSpec {
	seen := make(map[string]struct{}, len(input))
	options := make([]spokeCommandOptionSpec, 0, len(input))

	for _, raw := range input {
		name := strings.ToLower(strings.TrimSpace(raw.Name))
		if name == "" {
			continue
		}
		if !slashCommandNameRegex.MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}

		optionType := normalizeSpokeOptionType(raw.Type)
		description := strings.TrimSpace(raw.Description)
		if description == "" {
			description = "Optional value"
		}

		options = append(options, spokeCommandOptionSpec{
			Name:        name,
			Type:        optionType,
			Description: description,
			Required:    raw.Required,
		})
		seen[name] = struct{}{}
	}

	return options
}

func normalizeSpokeOptionType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "string":
		return "string"
	case "integer", "int":
		return "integer"
	case "number", "float", "float64", "double":
		return "number"
	case "boolean", "bool":
		return "boolean"
	default:
		return "string"
	}
}

func discordOptionType(optionType string) discordgo.ApplicationCommandOptionType {
	switch optionType {
	case "integer":
		return discordgo.ApplicationCommandOptionInteger
	case "number":
		return discordgo.ApplicationCommandOptionNumber
	case "boolean":
		return discordgo.ApplicationCommandOptionBoolean
	default:
		return discordgo.ApplicationCommandOptionString
	}
}

func (b *spokeCommandBridge) CommandNames() []string {
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

func (b *spokeCommandBridge) OwnsCommand(name string) bool {
	if b == nil {
		return false
	}

	_, ok := b.commands[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (b *spokeCommandBridge) BuildDiscordCommands() []*discordgo.ApplicationCommand {
	if b == nil {
		return nil
	}

	commands := b.CommandNames()
	result := make([]*discordgo.ApplicationCommand, 0, len(commands))

	for _, commandName := range commands {
		spec := b.commands[commandName]

		discordOptions := make([]*discordgo.ApplicationCommandOption, 0, len(spec.Options))
		for _, option := range spec.Options {
			discordOptions = append(discordOptions, &discordgo.ApplicationCommandOption{
				Type:        discordOptionType(option.Type),
				Name:        option.Name,
				Description: option.Description,
				Required:    option.Required,
			})
		}

		result = append(result, &discordgo.ApplicationCommand{
			Name:        spec.Name,
			Description: spec.Description,
			Options:     discordOptions,
		})
	}

	return result
}

func (b *spokeCommandBridge) ExecuteCommand(ctx context.Context, commandName string, options map[string]any) (string, error) {
	if b == nil {
		return "", errors.New("spoke command bridge is disabled")
	}

	request := spokeCommandRequest{Command: commandName}
	if len(options) > 0 {
		request.Options = pruneCommandOptions(options)
	}

	if argument, ok := request.Options[legacySpokeArgumentOption].(string); ok {
		request.Argument = strings.TrimSpace(argument)
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.commandURL, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return "", err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return "", errors.New(message)
	}

	var commandResponse spokeCommandResponse
	if err := json.Unmarshal(body, &commandResponse); err != nil {
		return "", fmt.Errorf("invalid spoke command response: %w", err)
	}

	message := strings.TrimSpace(commandResponse.Message)
	if message == "" {
		message = fmt.Sprintf("Command `%s` acknowledged.", commandName)
	}

	return truncateForDiscord(message), nil
}

func pruneCommandOptions(input map[string]any) map[string]any {
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

func truncateForDiscord(message string) string {
	if len(message) <= discordResponseCharacterLimit {
		return message
	}

	if discordResponseCharacterLimit < 4 {
		return message[:discordResponseCharacterLimit]
	}

	return message[:discordResponseCharacterLimit-3] + "..."
}

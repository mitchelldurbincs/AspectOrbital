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
	defaultSpokeCommandsURL       = "http://127.0.0.1:8090/control/commands"
	defaultSpokeCommandURL        = "http://127.0.0.1:8090/control/command"
	spokeCommandArgumentOption    = "argument"
	spokeDiscoveryAttemptCount    = 8
	spokeDiscoveryAttemptDelay    = 2 * time.Second
	spokeCommandHTTPTimeout       = 8 * time.Second
	spokeCommandDescription       = "Command owned by beeminder-spoke"
	spokeArgumentOptionHelp       = "Optional argument, like 30m"
	spokeCommandFailurePrefix     = "Command failed: "
	discordResponseCharacterLimit = 1900
)

var slashCommandNameRegex = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type spokeCommandBridge struct {
	log         *log.Logger
	httpClient  *http.Client
	commandsURL string
	commandURL  string
	commands    map[string]struct{}
}

type spokeCommandCatalog struct {
	Commands []string `json:"commands"`
}

type spokeCommandRequest struct {
	Command  string `json:"command"`
	Argument string `json:"argument,omitempty"`
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
		commands:    make(map[string]struct{}),
	}

	commands, err := bridge.fetchCommandsWithRetry()
	if err != nil {
		logger.Printf("spoke command bridge unavailable: %v", err)
		return nil
	}

	for _, command := range commands {
		bridge.commands[command] = struct{}{}
	}

	logger.Printf("loaded %d beeminder command(s): %s", len(commands), strings.Join(commands, ", "))
	return bridge
}

func (b *spokeCommandBridge) fetchCommandsWithRetry() ([]string, error) {
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

func (b *spokeCommandBridge) fetchCommands(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.commandsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("spoke command catalog request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var catalog spokeCommandCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("spoke command catalog decode failed: %w", err)
	}

	commands := normalizeSpokeCommands(catalog.Commands)
	if len(commands) == 0 {
		return nil, errors.New("spoke command catalog returned no usable commands")
	}

	return commands, nil
}

func normalizeSpokeCommands(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	commands := make([]string, 0, len(input))

	for _, raw := range input {
		command := strings.ToLower(strings.TrimSpace(raw))
		if command == "" || command == pingCommandName {
			continue
		}
		if !slashCommandNameRegex.MatchString(command) {
			continue
		}
		if _, ok := seen[command]; ok {
			continue
		}

		seen[command] = struct{}{}
		commands = append(commands, command)
	}

	sort.Strings(commands)
	return commands
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

	for _, command := range commands {
		result = append(result, &discordgo.ApplicationCommand{
			Name:        command,
			Description: spokeCommandDescription,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        spokeCommandArgumentOption,
					Description: spokeArgumentOptionHelp,
					Required:    false,
				},
			},
		})
	}

	return result
}

func (b *spokeCommandBridge) ExecuteCommand(ctx context.Context, commandName, argument string) (string, error) {
	if b == nil {
		return "", errors.New("spoke command bridge is disabled")
	}

	requestBody, err := json.Marshal(spokeCommandRequest{
		Command:  commandName,
		Argument: argument,
	})
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

func truncateForDiscord(message string) string {
	if len(message) <= discordResponseCharacterLimit {
		return message
	}

	if discordResponseCharacterLimit < 4 {
		return message[:discordResponseCharacterLimit]
	}

	return message[:discordResponseCharacterLimit-3] + "..."
}

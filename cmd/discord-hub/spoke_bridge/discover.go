package spokebridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func Discover(logger *log.Logger) *Bridge {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_ENABLED")), "false") {
		logger.Println("spoke command bridge disabled by SPOKE_COMMANDS_ENABLED=false")
		return nil
	}

	commandsURL := strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_URL"))
	if commandsURL == "" {
		commandsURL = defaultCommandsURL
	}

	commandURL := strings.TrimSpace(os.Getenv("SPOKE_COMMAND_URL"))
	if commandURL == "" {
		commandURL = defaultCommandURL
	}

	bridge := &Bridge{
		log:         logger,
		httpClient:  &http.Client{Timeout: CommandHTTPTimeout},
		commandsURL: commandsURL,
		commandURL:  commandURL,
		commands:    make(map[string]CommandSpec),
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

func (b *Bridge) fetchCommandsWithRetry() ([]CommandSpec, error) {
	var lastErr error

	for attempt := 1; attempt <= discoveryAttemptCount; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), CommandHTTPTimeout)
		commands, err := b.fetchCommands(ctx)
		cancel()
		if err == nil {
			return commands, nil
		}

		lastErr = err
		if attempt < discoveryAttemptCount {
			b.log.Printf("waiting for beeminder-spoke command catalog (%d/%d): %v", attempt, discoveryAttemptCount, err)
			time.Sleep(discoveryAttemptDelay)
		}
	}

	return nil, lastErr
}

func (b *Bridge) FetchCommandsWithRetry() ([]CommandSpec, error) {
	return b.fetchCommandsWithRetry()
}

func (b *Bridge) fetchCommands(ctx context.Context) ([]CommandSpec, error) {
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

	commands, err := parseCommandCatalog(body)
	if err != nil {
		return nil, err
	}

	normalized := normalizeCommandSpecs(commands)
	if len(normalized) == 0 {
		return nil, errors.New("spoke command catalog returned no usable commands")
	}

	return normalized, nil
}

func (b *Bridge) FetchCommands(ctx context.Context) ([]CommandSpec, error) {
	return b.fetchCommands(ctx)
}

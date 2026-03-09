package spokebridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

func Discover(logger *log.Logger) *Bridge {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_ENABLED")), "false") {
		logger.Println("spoke command bridge disabled by SPOKE_COMMANDS_ENABLED=false")
		return nil
	}

	services, err := configuredServices()
	if err != nil {
		logger.Printf("spoke command bridge unavailable: %v", err)
		return nil
	}

	bridge := NewBridgeWithServices(logger, &http.Client{Timeout: CommandHTTPTimeout}, services, nil, nil)

	commands, owners, counts, err := bridge.fetchAllCommandsWithRetry()
	if err != nil {
		logger.Printf("spoke command bridge unavailable: %v", err)
		return nil
	}

	bridge.commands = commands
	bridge.commandOwners = owners

	parts := make([]string, 0, len(counts))
	for _, serviceName := range bridge.serviceOrder {
		if count, ok := counts[serviceName]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", serviceName, count))
		}
	}

	logger.Printf("loaded %d command(s) across %d service(s): %s", len(commands), len(parts), strings.Join(parts, ", "))
	return bridge
}

func configuredServices() ([]ServiceDefinition, error) {
	if raw := strings.TrimSpace(os.Getenv("SPOKE_COMMAND_SERVICES")); raw != "" {
		var services []ServiceDefinition
		if err := json.Unmarshal([]byte(raw), &services); err != nil {
			return nil, fmt.Errorf("invalid SPOKE_COMMAND_SERVICES: %w", err)
		}
		if len(services) == 0 {
			return nil, errors.New("SPOKE_COMMAND_SERVICES cannot be empty")
		}
		for i, service := range services {
			if strings.TrimSpace(service.CommandsURL) == "" {
				return nil, fmt.Errorf("SPOKE_COMMAND_SERVICES[%d].commandsUrl is required", i)
			}
			if strings.TrimSpace(service.ExecuteURL) == "" {
				return nil, fmt.Errorf("SPOKE_COMMAND_SERVICES[%d].executeUrl is required", i)
			}
		}
		return services, nil
	}

	commandsURL := strings.TrimSpace(os.Getenv("SPOKE_COMMANDS_URL"))
	commandURL := strings.TrimSpace(os.Getenv("SPOKE_COMMAND_URL"))
	if commandsURL == "" {
		return nil, errors.New("SPOKE_COMMANDS_URL is required when SPOKE_COMMAND_SERVICES is not set")
	}
	if commandURL == "" {
		return nil, errors.New("SPOKE_COMMAND_URL is required when SPOKE_COMMAND_SERVICES is not set")
	}

	return []ServiceDefinition{{
		Name:        defaultServiceName,
		CommandsURL: commandsURL,
		ExecuteURL:  commandURL,
	}}, nil
}

func (b *Bridge) fetchAllCommandsWithRetry() (map[string]CommandSpec, map[string]string, map[string]int, error) {
	allCommands := make(map[string]CommandSpec)
	owners := make(map[string]string)
	counts := make(map[string]int)

	for _, serviceName := range b.serviceOrder {
		service := b.services[serviceName]
		commands, err := b.fetchCommandsForServiceWithRetry(service)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("service %q catalog unavailable: %w", service.Name, err)
		}
		counts[service.Name] = len(commands)

		for _, command := range commands {
			if existingOwner, ok := owners[command.Name]; ok {
				return nil, nil, nil, fmt.Errorf("duplicate command %q provided by services %q and %q", command.Name, existingOwner, service.Name)
			}
			allCommands[command.Name] = command
			owners[command.Name] = service.Name
		}
	}

	return allCommands, owners, counts, nil
}

func (b *Bridge) fetchCommandsForServiceWithRetry(service ServiceDefinition) ([]CommandSpec, error) {
	var lastErr error

	for attempt := 1; attempt <= discoveryAttemptCount; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), CommandHTTPTimeout)
		commands, err := b.fetchCommandsForService(ctx, service)
		cancel()
		if err == nil {
			return commands, nil
		}

		lastErr = err
		if attempt < discoveryAttemptCount {
			b.log.Printf("waiting for %s command catalog (%d/%d): %v", service.Name, attempt, discoveryAttemptCount, err)
			time.Sleep(discoveryAttemptDelay)
		}
	}

	return nil, lastErr
}

func (b *Bridge) FetchCommandsWithRetry() ([]CommandSpec, error) {
	if len(b.serviceOrder) == 0 {
		return nil, nil
	}
	service := b.services[b.serviceOrder[0]]
	return b.fetchCommandsForServiceWithRetry(service)
}

func (b *Bridge) fetchCommandsForService(ctx context.Context, service ServiceDefinition) ([]CommandSpec, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, service.CommandsURL, nil)
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
		return nil, fmt.Errorf("spoke command catalog request failed for service %q (%s): %s", service.Name, resp.Status, strings.TrimSpace(string(body)))
	}

	commands, err := parseCommandCatalog(body)
	if err != nil {
		return nil, err
	}

	normalized := normalizeCommandSpecs(commands)
	if len(normalized) == 0 {
		return nil, fmt.Errorf("command catalog returned no usable commands for service %q", service.Name)
	}

	return normalized, nil
}

func (b *Bridge) FetchCommands(ctx context.Context) ([]CommandSpec, error) {
	if len(b.serviceOrder) == 0 {
		return nil, nil
	}
	service := b.services[b.serviceOrder[0]]
	return b.fetchCommandsForService(ctx, service)
}

func (b *Bridge) ServiceCommandCounts() map[string]int {
	counts := make(map[string]int)
	for name := range b.services {
		counts[name] = 0
	}
	for _, owner := range b.commandOwners {
		counts[owner]++
	}
	return counts
}

func (b *Bridge) SortedServices() []string {
	services := make([]string, 0, len(b.services))
	for name := range b.services {
		services = append(services, name)
	}
	sort.Strings(services)
	return services
}

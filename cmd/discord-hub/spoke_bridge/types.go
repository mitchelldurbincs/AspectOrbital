package spokebridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"personal-infrastructure/pkg/spokecontract"
)

const (
	discoveryAttemptCount         = 8
	discoveryAttemptDelay         = 2 * time.Second
	CommandHTTPTimeout            = 8 * time.Second
	commandFailurePrefix          = "Command failed: "
	discordResponseCharacterLimit = 1900
)

var slashCommandNameRegex = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Bridge struct {
	log           *log.Logger
	httpClient    *http.Client
	authToken     string
	services      map[string]ServiceDefinition
	serviceOrder  []string
	commandOwners map[string]string
	commands      map[string]CommandSpec
}

type ServiceDefinition struct {
	Name        string `json:"name"`
	CommandsURL string `json:"commandsUrl"`
	ExecuteURL  string `json:"executeUrl"`
}

type CommandCatalog = spokecontract.CommandCatalog
type CommandSpec = spokecontract.CommandSpec
type CommandOptionSpec = spokecontract.CommandOptionSpec
type CommandContext = spokecontract.CommandContext

type commandRequest struct {
	Command string         `json:"command"`
	Context CommandContext `json:"context"`
	Options map[string]any `json:"options,omitempty"`
}

type commandResponse struct {
	Status  string          `json:"status"`
	Command string          `json:"command"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewBridge(logger *log.Logger, httpClient *http.Client, authToken, commandsURL, commandURL string, commands map[string]CommandSpec) (*Bridge, error) {
	service := ServiceDefinition{
		Name:        "default",
		CommandsURL: commandsURL,
		ExecuteURL:  commandURL,
	}

	return NewBridgeWithServices(logger, httpClient, strings.TrimSpace(authToken), []ServiceDefinition{service}, commands, nil)
}

func NewBridgeWithServices(logger *log.Logger, httpClient *http.Client, authToken string, services []ServiceDefinition, commands map[string]CommandSpec, commandOwners map[string]string) (*Bridge, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: CommandHTTPTimeout}
	}

	normalizedServices, err := normalizeServices(services)
	if err != nil {
		return nil, err
	}
	serviceMap := make(map[string]ServiceDefinition, len(normalizedServices))
	serviceOrder := make([]string, 0, len(normalizedServices))
	for _, service := range normalizedServices {
		if _, exists := serviceMap[service.Name]; exists {
			return nil, fmt.Errorf("duplicate service name %q", service.Name)
		}
		serviceMap[service.Name] = service
		serviceOrder = append(serviceOrder, service.Name)
	}

	copyCommands := make(map[string]CommandSpec, len(commands))
	owners := make(map[string]string, len(commands))
	for key, value := range commands {
		commandName := strings.TrimSpace(key)
		if commandName == "" {
			return nil, errors.New("command map contains an empty command name")
		}
		if commandName != value.Name {
			return nil, fmt.Errorf("command map key %q must match command spec name %q", commandName, value.Name)
		}
		copyCommands[commandName] = value
		if owner := strings.TrimSpace(commandOwners[commandName]); owner != "" {
			if _, ok := serviceMap[owner]; !ok {
				return nil, fmt.Errorf("command %q references unknown service %q", commandName, owner)
			}
			owners[commandName] = owner
			continue
		}
		if len(normalizedServices) == 0 {
			return nil, fmt.Errorf("command %q has no owning service", commandName)
		}
		owners[commandName] = normalizedServices[0].Name
	}

	return &Bridge{
		log:           logger,
		httpClient:    httpClient,
		authToken:     strings.TrimSpace(authToken),
		services:      serviceMap,
		serviceOrder:  serviceOrder,
		commandOwners: owners,
		commands:      copyCommands,
	}, nil
}

func normalizeServices(services []ServiceDefinition) ([]ServiceDefinition, error) {
	if len(services) == 0 {
		return nil, errors.New("at least one service definition is required")
	}

	normalized := make([]ServiceDefinition, 0, len(services))
	for idx, service := range services {
		name := strings.TrimSpace(service.Name)
		if name == "" {
			return nil, fmt.Errorf("services[%d].name is required", idx)
		}
		commandsURL := strings.TrimSpace(service.CommandsURL)
		if commandsURL == "" {
			return nil, fmt.Errorf("services[%d].commandsUrl is required", idx)
		}
		executeURL := strings.TrimSpace(service.ExecuteURL)
		if executeURL == "" {
			return nil, fmt.Errorf("services[%d].executeUrl is required", idx)
		}

		normalized = append(normalized, ServiceDefinition{
			Name:        name,
			CommandsURL: commandsURL,
			ExecuteURL:  executeURL,
		})
	}

	return normalized, nil
}

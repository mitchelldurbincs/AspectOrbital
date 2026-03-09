package spokebridge

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"personal-infrastructure/pkg/spokecontract"
)

const (
	defaultCommandsURL            = "http://127.0.0.1:8090/control/commands"
	defaultCommandURL             = "http://127.0.0.1:8090/control/command"
	defaultServiceName            = "default"
	discoveryAttemptCount         = 8
	discoveryAttemptDelay         = 2 * time.Second
	CommandHTTPTimeout            = 8 * time.Second
	defaultCommandDescription     = "Command owned by configured spoke service"
	commandFailurePrefix          = "Command failed: "
	discordResponseCharacterLimit = 1900
)

var slashCommandNameRegex = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Bridge struct {
	log           *log.Logger
	httpClient    *http.Client
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

func NewBridge(logger *log.Logger, httpClient *http.Client, commandsURL, commandURL string, commands map[string]CommandSpec) *Bridge {
	service := ServiceDefinition{
		Name:        defaultServiceName,
		CommandsURL: commandsURL,
		ExecuteURL:  commandURL,
	}

	return NewBridgeWithServices(logger, httpClient, []ServiceDefinition{service}, commands, nil)
}

func NewBridgeWithServices(logger *log.Logger, httpClient *http.Client, services []ServiceDefinition, commands map[string]CommandSpec, commandOwners map[string]string) *Bridge {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: CommandHTTPTimeout}
	}

	normalizedServices := normalizeServices(services)
	serviceMap := make(map[string]ServiceDefinition, len(normalizedServices))
	serviceOrder := make([]string, 0, len(normalizedServices))
	for _, service := range normalizedServices {
		serviceMap[service.Name] = service
		serviceOrder = append(serviceOrder, service.Name)
	}

	copyCommands := make(map[string]CommandSpec, len(commands))
	owners := make(map[string]string, len(commands))
	for key, value := range commands {
		commandName := strings.ToLower(strings.TrimSpace(key))
		if commandName == "" {
			continue
		}
		copyCommands[commandName] = value
		if owner := strings.TrimSpace(commandOwners[commandName]); owner != "" {
			owners[commandName] = owner
			continue
		}
		owners[commandName] = normalizedServices[0].Name
	}

	return &Bridge{
		log:           logger,
		httpClient:    httpClient,
		services:      serviceMap,
		serviceOrder:  serviceOrder,
		commandOwners: owners,
		commands:      copyCommands,
	}
}

func normalizeServices(services []ServiceDefinition) []ServiceDefinition {
	if len(services) == 0 {
		return []ServiceDefinition{{
			Name:        defaultServiceName,
			CommandsURL: defaultCommandsURL,
			ExecuteURL:  defaultCommandURL,
		}}
	}

	normalized := make([]ServiceDefinition, 0, len(services))
	for idx, service := range services {
		name := strings.TrimSpace(service.Name)
		if name == "" {
			if len(services) == 1 {
				name = defaultServiceName
			} else {
				name = "service-" + strconv.Itoa(idx+1)
			}
		}
		commandsURL := strings.TrimSpace(service.CommandsURL)
		if commandsURL == "" {
			commandsURL = defaultCommandsURL
		}
		executeURL := strings.TrimSpace(service.ExecuteURL)
		if executeURL == "" {
			executeURL = defaultCommandURL
		}

		normalized = append(normalized, ServiceDefinition{
			Name:        name,
			CommandsURL: commandsURL,
			ExecuteURL:  executeURL,
		})
	}

	return normalized
}

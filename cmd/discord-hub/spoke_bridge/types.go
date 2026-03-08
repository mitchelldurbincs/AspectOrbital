package spokebridge

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"
)

const (
	defaultCommandsURL            = "http://127.0.0.1:8090/control/commands"
	defaultCommandURL             = "http://127.0.0.1:8090/control/command"
	legacyArgumentOption          = "argument"
	discoveryAttemptCount         = 8
	discoveryAttemptDelay         = 2 * time.Second
	CommandHTTPTimeout            = 8 * time.Second
	legacyCommandDescription      = "Command owned by beeminder-spoke"
	legacyArgumentOptionHelp      = "Optional argument, like 30m"
	defaultCommandOptionType      = "string"
	commandFailurePrefix          = "Command failed: "
	discordResponseCharacterLimit = 1900
)

var slashCommandNameRegex = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Bridge struct {
	log         *log.Logger
	httpClient  *http.Client
	commandsURL string
	commandURL  string
	commands    map[string]CommandSpec
}

type CommandCatalog struct {
	Version  int           `json:"version"`
	Service  string        `json:"service"`
	Commands []CommandSpec `json:"commands"`
	Names    []string      `json:"commandNames,omitempty"`
}

type legacyCommandCatalog struct {
	Commands []string `json:"commands"`
}

type CommandSpec struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Options     []CommandOptionSpec `json:"options,omitempty"`
}

type CommandOptionSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type commandRequest struct {
	Command  string         `json:"command"`
	Argument string         `json:"argument,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type commandResponse struct {
	Status  string          `json:"status"`
	Command string          `json:"command"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewBridge(logger *log.Logger, httpClient *http.Client, commandsURL, commandURL string, commands map[string]CommandSpec) *Bridge {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: CommandHTTPTimeout}
	}

	copyCommands := make(map[string]CommandSpec, len(commands))
	for key, value := range commands {
		copyCommands[key] = value
	}

	if commandsURL == "" {
		commandsURL = defaultCommandsURL
	}
	if commandURL == "" {
		commandURL = defaultCommandURL
	}

	return &Bridge{
		log:         logger,
		httpClient:  httpClient,
		commandsURL: commandsURL,
		commandURL:  commandURL,
		commands:    copyCommands,
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
)

const (
	defaultSpokeCommandsURL       = "http://127.0.0.1:8090/control/commands"
	defaultSpokeCommandURL        = "http://127.0.0.1:8090/control/command"
	legacySpokeArgumentOption     = "argument"
	legacySpokeCommandDescription = "Command owned by beeminder-spoke"
	spokeCommandHTTPTimeout       = spokebridge.CommandHTTPTimeout
	discordResponseCharacterLimit = 1900
)

type spokeCommandCatalog = spokebridge.CommandCatalog
type spokeCommandSpec = spokebridge.CommandSpec
type spokeCommandOptionSpec = spokebridge.CommandOptionSpec

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

type spokeCommandBridge struct {
	log         *log.Logger
	httpClient  *http.Client
	commandsURL string
	commandURL  string
	commands    map[string]spokeCommandSpec

	inner *spokebridge.Bridge
}

func discoverSpokeCommandBridge(logger *log.Logger) *spokeCommandBridge {
	inner := spokebridge.Discover(logger)
	if inner == nil {
		return nil
	}

	return &spokeCommandBridge{inner: inner}
}

func (b *spokeCommandBridge) delegate() *spokebridge.Bridge {
	if b == nil {
		return nil
	}
	if b.inner != nil {
		return b.inner
	}

	return spokebridge.NewBridge(b.log, b.httpClient, b.commandsURL, b.commandURL, b.commands)
}

func (b *spokeCommandBridge) fetchCommandsWithRetry() ([]spokeCommandSpec, error) {
	delegate := b.delegate()
	if delegate == nil {
		return nil, nil
	}

	return delegate.FetchCommandsWithRetry()
}

func (b *spokeCommandBridge) fetchCommands(ctx context.Context) ([]spokeCommandSpec, error) {
	delegate := b.delegate()
	if delegate == nil {
		return nil, nil
	}

	return delegate.FetchCommands(ctx)
}

func (b *spokeCommandBridge) CommandNames() []string {
	delegate := b.delegate()
	if delegate == nil {
		return nil
	}

	return delegate.CommandNames()
}

func (b *spokeCommandBridge) OwnsCommand(name string) bool {
	delegate := b.delegate()
	if delegate == nil {
		return false
	}

	return delegate.OwnsCommand(name)
}

func (b *spokeCommandBridge) BuildDiscordCommands() []*discordgo.ApplicationCommand {
	delegate := b.delegate()
	if delegate == nil {
		return nil
	}

	return delegate.BuildDiscordCommands()
}

func (b *spokeCommandBridge) ExecuteCommand(ctx context.Context, commandName string, options map[string]any) (string, error) {
	delegate := b.delegate()
	if delegate == nil {
		return "", errors.New("spoke command bridge is disabled")
	}

	return delegate.ExecuteCommand(ctx, commandName, options)
}

func parseSpokeCommandCatalog(body []byte) ([]spokeCommandSpec, error) {
	return spokebridge.ParseCommandCatalog(body)
}

func normalizeSpokeCommandSpecs(input []spokeCommandSpec) []spokeCommandSpec {
	return spokebridge.NormalizeCommandSpecs(input)
}

func normalizeSpokeOptionType(raw string) string {
	return spokebridge.NormalizeOptionType(raw)
}

func discordOptionType(optionType string) discordgo.ApplicationCommandOptionType {
	return spokebridge.DiscordOptionType(optionType)
}

func pruneCommandOptions(input map[string]any) map[string]any {
	return spokebridge.PruneCommandOptions(input)
}

func truncateForDiscord(message string) string {
	return spokebridge.TruncateForDiscord(message)
}

func formatSpokeCommandFailure(err error) string {
	return spokebridge.FormatCommandFailure(err)
}

package spokecontrol

import (
	"fmt"
	"sort"
	"strings"

	"personal-infrastructure/pkg/spokecontract"
)

type Request = spokecontract.CommandRequest
type Catalog = spokecontract.CommandCatalog
type Command = spokecontract.CommandSpec
type CommandOption = spokecontract.CommandOptionSpec

func ValidateDiscordUser(req Request) error {
	if strings.TrimSpace(req.Context.DiscordUserID) == "" {
		return fmt.Errorf("context.discordUserId is required")
	}
	return nil
}

func NormalizeCommand(req Request) string {
	return spokecontract.NormalizeCommandName(req.Command)
}

func OK(command string, message string, data any) map[string]any {
	payload := map[string]any{
		"status":  "ok",
		"command": command,
		"message": message,
	}
	if data != nil {
		payload["data"] = data
	}
	return payload
}

func UnknownCommandError(requested string, valid []string) string {
	commands := append([]string(nil), valid...)
	sort.Strings(commands)
	return fmt.Sprintf("unknown command %q; valid commands: %s", requested, strings.Join(commands, ", "))
}

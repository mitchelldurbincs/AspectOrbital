package spokebridge

import "github.com/bwmarrin/discordgo"

func DiscordOptionType(optionType string) discordgo.ApplicationCommandOptionType {
	switch optionType {
	case "integer":
		return discordgo.ApplicationCommandOptionInteger
	case "number":
		return discordgo.ApplicationCommandOptionNumber
	case "boolean":
		return discordgo.ApplicationCommandOptionBoolean
	case "attachment":
		return discordgo.ApplicationCommandOptionAttachment
	default:
		return discordgo.ApplicationCommandOptionString
	}
}

func (b *Bridge) BuildDiscordCommands() []*discordgo.ApplicationCommand {
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
				Type:        DiscordOptionType(option.Type),
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

func TruncateForDiscord(message string) string {
	if len(message) <= discordResponseCharacterLimit {
		return message
	}

	if discordResponseCharacterLimit < 4 {
		return message[:discordResponseCharacterLimit]
	}

	return message[:discordResponseCharacterLimit-3] + "..."
}

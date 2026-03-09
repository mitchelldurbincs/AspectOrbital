package spokecontract

import "testing"

func TestValidateCatalogSchema(t *testing.T) {
	catalog := CommandCatalog{
		Version: CatalogVersion,
		Service: "beeminder-spoke",
		Commands: []CommandSpec{{
			Name:        "status",
			Description: "Show status",
			Options: []CommandOptionSpec{{
				Name:        "duration",
				Type:        "string",
				Description: "Snooze duration",
				Required:    false,
			}},
		}},
	}

	if err := ValidateCatalogSchema(catalog); err != nil {
		t.Fatalf("ValidateCatalogSchema() error = %v", err)
	}
}

func TestValidateCatalogSchemaRejectsInvalidType(t *testing.T) {
	catalog := CommandCatalog{
		Version: CatalogVersion,
		Service: "beeminder-spoke",
		Commands: []CommandSpec{{
			Name:        "status",
			Description: "Show status",
			Options: []CommandOptionSpec{{
				Name:        "duration",
				Type:        "duration",
				Description: "Snooze duration",
				Required:    false,
			}},
		}},
	}

	if err := ValidateCatalogSchema(catalog); err == nil {
		t.Fatal("expected schema validation error for invalid option type")
	}
}

func TestValidateCommandRequestSchema(t *testing.T) {
	req := CommandRequest{
		Command: "status",
		Context: CommandContext{
			DiscordUserID: "123",
			GuildID:       "456",
			ChannelID:     "789",
		},
		Options: map[string]any{"duration": "30m"},
	}

	if err := ValidateCommandRequestSchema(req); err != nil {
		t.Fatalf("ValidateCommandRequestSchema() error = %v", err)
	}
}

func TestValidateCommandRequestSchemaRejectsMissingUser(t *testing.T) {
	req := CommandRequest{Command: "status", Context: CommandContext{}}

	if err := ValidateCommandRequestSchema(req); err == nil {
		t.Fatal("expected schema validation error for missing discord user id")
	}
}

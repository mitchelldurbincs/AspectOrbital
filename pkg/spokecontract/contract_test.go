package spokecontract

import (
	"strings"
	"testing"
)

func TestValidateCatalogRejectsDuplicateCommandNames(t *testing.T) {
	catalog := CommandCatalog{
		Version: CatalogVersion,
		Service: "beeminder-spoke",
		Commands: []CommandSpec{
			{
				Name:        "status",
				Description: "Show status",
			},
			{
				Name:        "status",
				Description: "Show status again",
			},
		},
	}

	err := ValidateCatalog(catalog)
	if err == nil {
		t.Fatal("expected duplicate command name error")
	}
	if !strings.Contains(err.Error(), "duplicate command name \"status\"") {
		t.Fatalf("expected duplicate command name error, got %v", err)
	}
}

func TestValidateCatalogRejectsDuplicateOptionNames(t *testing.T) {
	catalog := CommandCatalog{
		Version: CatalogVersion,
		Service: "beeminder-spoke",
		Commands: []CommandSpec{{
			Name:        "status",
			Description: "Show status",
			Options: []CommandOptionSpec{
				{
					Name:        "duration",
					Type:        "string",
					Description: "Snooze duration",
				},
				{
					Name:        "duration",
					Type:        "string",
					Description: "Another duration",
				},
			},
		}},
	}

	err := ValidateCatalog(catalog)
	if err == nil {
		t.Fatal("expected duplicate option name error")
	}
	if !strings.Contains(err.Error(), "duplicate option name \"duration\" for command \"status\"") {
		t.Fatalf("expected duplicate option name error, got %v", err)
	}
}

func TestValidateCatalogRejectsCommandNamesWithWhitespace(t *testing.T) {
	catalog := CommandCatalog{
		Version:  CatalogVersion,
		Service:  "beeminder-spoke",
		Commands: []CommandSpec{{Name: " status ", Description: "Show status"}},
	}

	err := ValidateCatalog(catalog)
	if err == nil {
		t.Fatal("expected whitespace validation error")
	}
	if !strings.Contains(err.Error(), "must not include leading or trailing spaces") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCatalogRejectsOptionTypeAliases(t *testing.T) {
	catalog := CommandCatalog{
		Version: CatalogVersion,
		Service: "beeminder-spoke",
		Commands: []CommandSpec{{
			Name:        "status",
			Description: "Show status",
			Options:     []CommandOptionSpec{{Name: "duration", Type: "int", Description: "Bad alias"}},
		}},
	}

	err := ValidateCatalog(catalog)
	if err == nil {
		t.Fatal("expected option type validation error")
	}
	if !strings.Contains(err.Error(), "invalid option type \"int\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCatalogDelegatesSchemaValidation(t *testing.T) {
	catalog := CommandCatalog{
		Version: CatalogVersion,
		Commands: []CommandSpec{{
			Name:        "status",
			Description: "Show status",
		}},
	}

	err := ValidateCatalog(catalog)
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if !strings.Contains(err.Error(), "service is required") {
		t.Fatalf("expected schema error to be returned, got %v", err)
	}
}

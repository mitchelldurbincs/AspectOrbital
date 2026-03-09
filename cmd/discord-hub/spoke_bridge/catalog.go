package spokebridge

import (
	"encoding/json"

	"personal-infrastructure/pkg/spokecontract"
)

func parseCommandCatalog(body []byte) ([]CommandSpec, error) {
	var catalog CommandCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, err
	}

	if err := spokecontract.ValidateCatalog(catalog); err != nil {
		return nil, err
	}

	return catalog.Commands, nil
}

func ParseCommandCatalog(body []byte) ([]CommandSpec, error) {
	return parseCommandCatalog(body)
}

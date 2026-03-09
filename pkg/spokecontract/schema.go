package spokecontract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

const contractSchemaID = "https://aspectorbital.local/contracts/spoke-contract-v1.schema.json"

var (
	contractSchemaJSON     []byte
	contractSchemaJSONErr  error
	loadContractSchemaOnce sync.Once
)

func loadContractSchemaJSON() ([]byte, error) {
	loadContractSchemaOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			contractSchemaJSONErr = fmt.Errorf("failed to resolve schema path")
			return
		}

		schemaPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "contracts", "spoke-contract-v1.schema.json"))
		contractSchemaJSON, contractSchemaJSONErr = os.ReadFile(schemaPath)
	})

	if contractSchemaJSONErr != nil {
		return nil, contractSchemaJSONErr
	}

	return contractSchemaJSON, nil
}

func validateAgainstSchemaRef(data any, schemaRef string) error {
	schemaBytes, err := loadContractSchemaJSON()
	if err != nil {
		return fmt.Errorf("failed to load contract schema: %w", err)
	}

	loader := gojsonschema.NewSchemaLoader()
	loader.AutoDetect = true
	if err := loader.AddSchemas(gojsonschema.NewStringLoader(string(schemaBytes))); err != nil {
		return fmt.Errorf("failed to load root schema: %w", err)
	}

	compiled, err := loader.Compile(gojsonschema.NewReferenceLoader(contractSchemaID + "#/$defs/" + schemaRef))
	if err != nil {
		return fmt.Errorf("failed to compile schema ref %q: %w", schemaRef, err)
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	result, err := compiled.Validate(gojsonschema.NewBytesLoader(payload))
	if err != nil {
		return err
	}
	if result.Valid() {
		return nil
	}

	issues := make([]string, 0, len(result.Errors()))
	for _, issue := range result.Errors() {
		field := strings.TrimSpace(issue.Field())
		if field == "(root)" {
			issues = append(issues, issue.Description())
			continue
		}
		issues = append(issues, fmt.Sprintf("%s: %s", field, issue.Description()))
	}
	sort.Strings(issues)

	return fmt.Errorf("schema validation failed for %s: %s", schemaRef, strings.Join(issues, "; "))
}

func ValidateCatalogSchema(catalog CommandCatalog) error {
	return validateAgainstSchemaRef(catalog, "commandCatalog")
}

func ValidateCommandRequestSchema(request CommandRequest) error {
	return validateAgainstSchemaRef(request, "commandExecuteRequest")
}

func ValidateCommandResponseSchema(response CommandResponse) error {
	return validateAgainstSchemaRef(response, "commandExecuteResponse")
}

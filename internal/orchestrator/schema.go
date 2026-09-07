package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SchemaValidator validates a stage's structured output file against one of
// the schemas committed under schemas/. Compiled schemas are cached: a
// cohort run validates the same handful of schemas repeatedly across many
// cases, and schema files are immutable for the lifetime of a protocol
// version.
type SchemaValidator struct {
	schemasRoot string

	mu     sync.Mutex
	cached map[string]*jsonschema.Schema
}

func NewSchemaValidator(schemasRoot string) *SchemaValidator {
	return &SchemaValidator{
		schemasRoot: schemasRoot,
		cached:      make(map[string]*jsonschema.Schema),
	}
}

// ValidateFile checks that the JSON document at dataPath satisfies the named
// schema (e.g. "review-a.schema.json", relative to the validator's
// schemasRoot). It never mutates dataPath or the schema.
func (v *SchemaValidator) ValidateFile(dataPath, schemaName string) error {
	schema, err := v.compiled(schemaName)
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(dataPath)
	if err != nil {
		return fmt.Errorf("orchestrator: reading %s: %w", dataPath, err)
	}
	var doc interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("orchestrator: %s is not valid JSON: %w", dataPath, err)
	}
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("orchestrator: %s does not satisfy %s: %w", dataPath, schemaName, err)
	}
	return nil
}

func (v *SchemaValidator) compiled(schemaName string) (*jsonschema.Schema, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if s, ok := v.cached[schemaName]; ok {
		return s, nil
	}
	path := filepath.Join(v.schemasRoot, schemaName)
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: compiling schema %s: %w", path, err)
	}
	v.cached[schemaName] = schema
	return schema, nil
}

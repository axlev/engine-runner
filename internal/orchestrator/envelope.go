package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
)

// schemaVersionOf reads the schema_version a stage output must declare from
// the output schema itself - its properties.schema_version.const.
//
// This used to be a map keyed by STAGE NAME (reasoner-1 -> review-a/v1),
// which was correct only while every protocol produced the same three
// documents. H1 protocols reuse the stage names and validate against
// different schemas: reasoner-1 under h1-g-v1 must be stamped
// h1-review-a/v1, not review-a/v1, or it fails its own validation on the
// first paid run. The protocol already declares which schema each stage
// writes; the stamp follows that declaration, so the version and the
// validator can never disagree about which document this is.
//
// Fails closed: a stage output schema that does not pin a schema_version
// const is a schema authoring error, refused before any stage runs.
func schemaVersionOf(schemaPath string) (string, error) {
	if v, ok := schemaVersionCache.Load(schemaPath); ok {
		return v.(string), nil
	}
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return "", fmt.Errorf("reading output schema %s: %w", schemaPath, err)
	}
	var doc struct {
		Properties struct {
			SchemaVersion struct {
				Const string `json:"const"`
			} `json:"schema_version"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("parsing output schema %s: %w", schemaPath, err)
	}
	if doc.Properties.SchemaVersion.Const == "" {
		return "", fmt.Errorf("output schema %s does not pin properties.schema_version.const; every stage output schema must", schemaPath)
	}
	schemaVersionCache.Store(schemaPath, doc.Properties.SchemaVersion.Const)
	return doc.Properties.SchemaVersion.Const, nil
}

var schemaVersionCache sync.Map

// stampEnvelope fills in the identity fields of a stage's output document.
//
// These five fields - schema_version, run_id, case_id, stage, generated_at -
// are engine facts, not review content. A reasoner cannot know the run id it
// was invoked under, so requiring it to emit one would fail every real
// invocation on a formatting error rather than a review error. Worse, a
// model that guessed would produce a schema-valid artifact whose
// self-reported identity disagrees with its own directory and the run
// manifest, and nothing downstream would catch it.
//
// So the reasoner authors only content (findings / assessments / verdicts)
// and the engine stamps the rest. Values are OVERWRITTEN rather than filled
// in when absent: the model must not be able to misstate its own identity
// even by trying.
//
// Deliberately not done here: stripping markdown fences. A model that wraps
// its JSON in a code fence has ignored an explicit instruction, and that
// should fail loudly and burn the retry rather than be papered over - the
// signal belongs to whoever maintains the prompt.
func stampEnvelope(path, runID, caseID string, stage adapters.Stage, schemaVersion string, now time.Time) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("stamping envelope: reading %s: %w", path, err)
	}

	// UseNumber keeps numeric literals exactly as written. Without it a line
	// number round-trips through float64 and can re-marshal as 1e+06, which
	// then fails the schemas' "integer" constraint.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]interface{}
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("stamping envelope: %s is not a JSON object: %w", path, err)
	}

	if schemaVersion == "" {
		return fmt.Errorf("stamping envelope: no schema_version supplied for stage %q", stage)
	}

	doc["schema_version"] = schemaVersion
	doc["run_id"] = runID
	doc["case_id"] = caseID
	doc["stage"] = string(stage)
	doc["generated_at"] = now.UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("stamping envelope: marshaling %s: %w", path, err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("stamping envelope: writing %s: %w", path, err)
	}
	return nil
}

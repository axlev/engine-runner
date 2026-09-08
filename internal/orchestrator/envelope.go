package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
)

// envelopeSchemaVersions maps a stage to the schema_version its output
// document must declare.
var envelopeSchemaVersions = map[adapters.Stage]string{
	adapters.StageReasoner1: "review-a/v1",
	adapters.StageReasoner2: "review-b/v1",
	adapters.StageReasoner3: "review-c/v1",
}

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
func stampEnvelope(path, runID, caseID string, stage adapters.Stage, now time.Time) error {
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

	schemaVersion, ok := envelopeSchemaVersions[stage]
	if !ok {
		return fmt.Errorf("stamping envelope: unknown stage %q", stage)
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

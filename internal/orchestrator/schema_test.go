package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

// The vendor projection drops h1-review-a's top-level allOf because the API
// rejects root combinators. That is only defensible if the CONSTRAINT is
// still enforced here, on the response. This test is the evidence for that
// claim rather than an assertion of it: a review-a with no findings and no
// empty_reason must be rejected, exactly the case the dropped conditional
// governs.
func TestDroppedVendorConditionalIsStillEnforcedOnTheResponse(t *testing.T) {
	dir := t.TempDir()
	v := NewSchemaValidator(filepath.Join("..", "..", "schemas"))

	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const base = `{"schema_version":"h1-review-a/v1","run_id":"r","case_id":"c",
		"stage":"reasoner-1","generated_at":"2026-09-17T00:00:00Z","summary":"s","findings":[]`

	// No findings and no empty_reason: the conditional must reject it.
	if err := v.ValidateFile(write("bad.json", base+"}"), "h1-review-a.schema.json"); err == nil {
		t.Error("an empty review-a with no empty_reason was accepted; the conditional " +
			"dropped from the vendor projection is NOT enforced on the response, so " +
			"dropping it weakened the contract")
	}
	// The same document with empty_reason must be accepted, so the test is
	// failing on the conditional and not on something incidental.
	if err := v.ValidateFile(write("good.json", base+`,"empty_reason":"nothing reviewable"}`),
		"h1-review-a.schema.json"); err != nil {
		t.Errorf("an empty review-a WITH empty_reason was rejected: %v", err)
	}
}

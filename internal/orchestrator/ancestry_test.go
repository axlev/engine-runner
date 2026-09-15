package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/contextbuilder"
)

func ancestryFixture(t *testing.T, aDoc, bDoc string) (error, []string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "review-a.json")
	b := filepath.Join(dir, "review-b.json")
	if err := os.WriteFile(a, []byte(aDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(bDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return validateAncestry(b, contextbuilder.StageInputs{Outputs: map[adapters.Stage]string{adapters.StageReasoner1: a}})
}

const discoveryDoc = `{"findings":[{"id":"f1","mechanism":"M1"},{"id":"f2","mechanism":"M2"}]}`

func TestAncestryAcceptsFaithfulSubstantiation(t *testing.T) {
	err, warnings := ancestryFixture(t, discoveryDoc,
		`{"assessments":[
		   {"finding_id":"f1","discovery_mechanism":"M1","disposition":"CONFIRMED","mechanism":"M1"},
		   {"finding_id":"f2","discovery_mechanism":"M2","disposition":"NARROWED","mechanism":"M2 but only when X"}]}`)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAncestryFailsClosed(t *testing.T) {
	cases := map[string]struct{ b, want string }{
		"finding with no ancestor": {
			`{"assessments":[{"finding_id":"f9","discovery_mechanism":"new","disposition":"CONFIRMED","mechanism":"new"}]}`,
			"no handed-over stage produced"},
		"discovery_mechanism restated": {
			`{"assessments":[{"finding_id":"f1","discovery_mechanism":"M1 (paraphrased)","disposition":"CONFIRMED","mechanism":"M1"}]}`,
			"restates discovery_mechanism"},
		"CONFIRMED with a changed mechanism": {
			`{"assessments":[{"finding_id":"f1","discovery_mechanism":"M1","disposition":"CONFIRMED","mechanism":"M1 but worse"}]}`,
			"CONFIRMED means the mechanism stands"},
		"NARROWED without narrowing": {
			`{"assessments":[{"finding_id":"f1","discovery_mechanism":"M1","disposition":"NARROWED","mechanism":"M1"}]}`,
			"NARROWED must state the tighter claim"},
	}
	for name, c := range cases {
		err, _ := ancestryFixture(t, discoveryDoc, c.b)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", name, c.want, err)
		}
	}
}

func TestAncestryWarnsOnDroppedFinding(t *testing.T) {
	err, warnings := ancestryFixture(t, discoveryDoc,
		`{"assessments":[{"finding_id":"f1","discovery_mechanism":"M1","disposition":"REJECTED"}]}`)
	if err != nil {
		t.Fatalf("dropping is not fatal: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"f2"`) {
		t.Errorf("want one warning naming f2, got %v", warnings)
	}
}

// pilot-v1's review-a has no mechanism field; the join still applies, the
// mechanism checks do not.
func TestAncestryOnPreH1ShapeChecksOnlyTheJoin(t *testing.T) {
	err, _ := ancestryFixture(t, `{"findings":[{"id":"f1"}]}`,
		`{"assessments":[{"finding_id":"f1","disposition":"CONFIRMED"}]}`)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	err, _ = ancestryFixture(t, `{"findings":[{"id":"f1"}]}`,
		`{"assessments":[{"finding_id":"ghost","disposition":"CONFIRMED"}]}`)
	if err == nil {
		t.Error("an unknown finding_id must fail even on the pre-H1 shape")
	}
}

func TestAncestryIgnoresStagesWithNoInputsOrNoAssessments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	_ = os.WriteFile(p, []byte(`{"new_findings":[{"id":"n1"}]}`), 0o644)
	if err, _ := validateAncestry(p, contextbuilder.StageInputs{}); err != nil {
		t.Errorf("no inputs: %v", err)
	}
	if err, _ := validateAncestry(p, contextbuilder.StageInputs{Outputs: map[adapters.Stage]string{adapters.StageReasoner1: p}}); err != nil {
		t.Errorf("no assessments: %v", err)
	}
}

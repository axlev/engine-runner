package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/orchestrator"
)

// A document in exactly the shape judge-mechanisms.schema.json defines.
// The test validates it against the real schema BEFORE parsing it, so this
// cannot drift into testing a shape the judge is not actually asked for:
// if the schema moves, validation fails here rather than in a paid run.
const mechanismsResponse = `{
  "schema_version": "judge-mechanisms/v1",
  "case_id": "case-abc123",
  "fix_mechanisms": [
    {"fix_id": "fix-1", "mechanism": "A nexthop cache entry was freed while still referenced, so an interface flap could dereference freed memory."},
    {"fix_id": "fix-2", "mechanism": "No identifiable defect repair; the change is a refactor."}
  ]
}`

func TestParseMechanismsRoundTripsTheRealSchema(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "mechanisms.json")
	if err := os.WriteFile(doc, []byte(mechanismsResponse), 0o644); err != nil {
		t.Fatal(err)
	}
	v := orchestrator.NewSchemaValidator(filepath.Join("..", "..", "schemas"))
	if err := v.ValidateFile(doc, "judge-mechanisms.schema.json"); err != nil {
		t.Fatalf("the fixture no longer satisfies the real schema: %v", err)
	}

	got, err := parseMechanisms([]byte(mechanismsResponse))
	if err != nil {
		t.Fatalf("parsing a schema-valid response failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d mechanisms from a 2-entry fix_mechanisms list; an empty list is the defect this guards", len(got))
	}
	if got[0].FixID != "fix-1" {
		t.Errorf("fix_id not carried: %+v", got[0])
	}
	if got[0].Description == "" {
		t.Fatal("mechanism text was dropped: the report's Description comes from the response's \"mechanism\" key, not \"description\"")
	}
}

// The exact defect: the old parser read {"mechanisms":[{"description":...}]}.
// A response in that shape must now yield nothing, so a regression to the
// old tags cannot pass by accident.
func TestParseMechanismsRejectsTheOldWrongShape(t *testing.T) {
	old := `{"mechanisms":[{"fix_id":"fix-1","description":"..."}]}`
	got, err := parseMechanisms([]byte(old))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the pre-fix wire shape parsed as %d mechanisms; the keys must come from the schema", len(got))
	}
}

func TestParseMechanismsSurfacesMalformedJSON(t *testing.T) {
	if _, err := parseMechanisms([]byte(`{"fix_mechanisms":`)); err == nil {
		t.Error("truncated JSON was accepted")
	}
}

// Resume must key on "was actually judged", not "a file exists": the defect
// sealed complete-looking reports carrying an empty fix list.
func TestSealedWithMechanismsTreatsAnEmptyListAsNotJudged(t *testing.T) {
	dir := t.TempDir()
	write := func(caseID, body string) {
		if err := os.WriteFile(filepath.Join(dir, caseID+".h1-judge.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("judged", `{"schema_version":"h1-judge/v1","case_id":"judged","mechanisms":[{"fix_id":"f","description":"d"}]}`)
	write("empty", `{"schema_version":"h1-judge/v1","case_id":"empty","mechanisms":[]}`)
	write("garbage", `{`)

	if !sealedWithMechanisms(dir, "judged") {
		t.Error("a judged report must be skipped on resume")
	}
	if sealedWithMechanisms(dir, "empty") {
		t.Error("a report with no mechanisms was never judged; resume must re-run it, not trust it")
	}
	if sealedWithMechanisms(dir, "garbage") {
		t.Error("an unreadable report must be re-judged")
	}
	if sealedWithMechanisms(dir, "absent") {
		t.Error("a case with no report must be judged")
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a , b ,, c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

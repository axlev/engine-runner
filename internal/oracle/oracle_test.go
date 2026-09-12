package oracle

import (
	"path/filepath"
	"testing"
)

// Every fixture under fixtures/oracle/ is hand-authored from the miner's
// model types (internal/model/candidate.go, retrospective.go). None is a
// copy of a real correlated-report.json: the real ones are the answer key
// for the pilot cohort, and whoever develops this ingest must stay able to
// perform blind disposition analysis afterwards. Isolation between the judge
// and reasoners 1-3 is a design requirement (system-design section 6.5); the
// same logic applies to the analyst.
func fixture(t *testing.T, name string) Evidence {
	t.Helper()
	ev, err := Load(filepath.Join("..", "..", "fixtures", "oracle", name+".json"), "case-"+name)
	if err != nil {
		t.Fatalf("Load(%s): %v", name, err)
	}
	return ev
}

func TestStrongestSignalWinsPerPath(t *testing.T) {
	ev := fixture(t, "mixed-tiers-same-path")
	p, ok := ev.ByPath("src/alpha.c")
	if !ok {
		t.Fatalf("expected evidence for src/alpha.c, got paths %+v", ev.Paths)
	}
	if p.Strength != StrengthStrong {
		t.Errorf("Strength = %v, want strong; a weak signal must not dilute a strong one", p.Strength)
	}
	// All three tiers named the path, so all three types are retained: the
	// type carries the meaning, and collapsing them would lose it.
	if len(p.SignalTypes) != 3 {
		t.Errorf("SignalTypes = %v, want all three retained", p.SignalTypes)
	}
	if ev.MaxStrength != StrengthStrong {
		t.Errorf("MaxStrength = %v, want strong", ev.MaxStrength)
	}
}

// A strong signal is a maintainer stating a later commit fixed this one. If
// it names no paths that is still the strongest evidence in the record, and
// dropping it for lack of a path would discard exactly the cases that matter
// most.
func TestStrongSignalWithNoPathsStillRaisesStrength(t *testing.T) {
	ev := fixture(t, "strong-no-paths")
	if ev.MaxStrength != StrengthStrong {
		t.Errorf("MaxStrength = %v, want strong even with no changed_paths", ev.MaxStrength)
	}
	if len(ev.Paths) != 0 {
		t.Errorf("Paths = %+v, want none; the record named no path", ev.Paths)
	}
}

func TestWeakOnlyDoesNotReadAsStrong(t *testing.T) {
	ev := fixture(t, "weak-only")
	if ev.MaxStrength != StrengthWeak {
		t.Fatalf("MaxStrength = %v, want weak", ev.MaxStrength)
	}
	// SAME_FILE_MODIFICATION means somebody touched the same file later.
	// On a repository whose changes touch few files that is close to no
	// information, which is the whole reason a same-file matching rule is
	// expected to be near-vacuous.
	for _, p := range ev.Paths {
		if p.Strength != StrengthWeak {
			t.Errorf("%s: Strength = %v, want weak", p.Path, p.Strength)
		}
	}
}

func TestNoSignalsIsNotAnError(t *testing.T) {
	ev := fixture(t, "no-signals")
	if ev.MaxStrength != StrengthNone {
		t.Errorf("MaxStrength = %v, want none", ev.MaxStrength)
	}
	if len(ev.Paths) != 0 {
		t.Errorf("Paths = %+v, want none", ev.Paths)
	}
	if ev.StatefulConflict {
		t.Errorf("a LOW-scored case with no signals is agreement, not conflict")
	}
}

// The miner's heuristic is a prediction and its retrospective evidence is an
// observation. Where they disagree the evaluator must say so rather than
// produce a middling number that hides it.
func TestStatefulConflictBothDirections(t *testing.T) {
	for _, name := range []string{"conflict-high-score-no-signal", "conflict-strong-signal-low-score"} {
		if ev := fixture(t, name); !ev.StatefulConflict {
			t.Errorf("%s: StatefulConflict = false, want true (score=%v category=%q maxStrength=%v)",
				name, ev.HeuristicScore, ev.HeuristicCategory, ev.MaxStrength)
		}
	}
}

// One signal naming two paths is the many-to-one shape from both directions:
// two reviewer findings can land on one signal, and one finding citing both
// files can land on two paths.
func TestOneSignalTwoPathsIndexesBoth(t *testing.T) {
	ev := fixture(t, "one-signal-two-paths")
	if len(ev.Paths) != 2 {
		t.Fatalf("Paths = %+v, want 2", ev.Paths)
	}
	a, _ := ev.ByPath("src/alpha.c")
	b, _ := ev.ByPath("src/beta.c")
	// Reversal overlap is the only quantity available for ranking paths by
	// how much of the original change the fix actually undid. It is a
	// count, never a location.
	if a.ReversalOverlap != 3 || b.ReversalOverlap != 11 {
		t.Errorf("ReversalOverlap alpha=%d beta=%d, want 3 and 11", a.ReversalOverlap, b.ReversalOverlap)
	}
}

// A defect path no reviewer finding names must survive ingest, or the
// evaluator can never report a missed defect.
func TestUnmatchedDefectPathIsRetained(t *testing.T) {
	ev := fixture(t, "unmatched-defect-path")
	if _, ok := ev.ByPath("src/never_reviewed.c"); !ok {
		t.Errorf("a path no finding will match must still be indexed, got %+v", ev.Paths)
	}
}

// The converse - a finding naming a path the oracle says nothing about -
// must be reported as absent rather than as an error or a zero-strength hit.
func TestFindingPathWithNoEvidenceIsAbsentNotZero(t *testing.T) {
	ev := fixture(t, "one-signal-two-paths")
	if _, ok := ev.ByPath("src/reviewer_invented_this.c"); ok {
		t.Errorf("expected no evidence for an unmentioned path")
	}
}

func TestMediumSymbolAndOrphanOverlap(t *testing.T) {
	ev := fixture(t, "medium-symbol-and-orphan-overlap")
	a, ok := ev.ByPath("src/alpha.c")
	if !ok {
		t.Fatalf("expected src/alpha.c")
	}
	// Only medium signals carry a symbol, which is why a file+symbol rule
	// is only partially supported by this record.
	if len(a.Symbols) != 1 || a.Symbols[0] != "alpha_init" {
		t.Errorf("Symbols = %v, want [alpha_init]", a.Symbols)
	}
	// A reversal overlap on a path no signal named is still evidence about
	// that path, at no attested strength.
	o, ok := ev.ByPath("src/orphan.c")
	if !ok {
		t.Fatalf("a reversal overlap on an unsignalled path must be retained")
	}
	if o.Strength != StrengthNone || o.ReversalOverlap != 5 {
		t.Errorf("orphan = %+v, want StrengthNone with overlap 5", o)
	}
}

func TestMediumSymbolWithNoPathsBecomesItsOwnKey(t *testing.T) {
	ev := fixture(t, "medium-symbol-no-paths")
	if _, ok := ev.ByPath("lonely_symbol"); !ok {
		t.Errorf("a function_or_path with no changed_paths must not be dropped, got %+v", ev.Paths)
	}
}

func TestLoadRejectsSomethingThatIsNotACorrelatedRecord(t *testing.T) {
	// An empty JSON object parses fine but is not a correlated record.
	// Accepting it would let a misconfigured path silently score every case
	// as "no evidence".
	dir := t.TempDir()
	p := filepath.Join(dir, "bogus.json")
	if err := writeFile(p, "{}"); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if _, err := Load(p, "case-bogus"); err == nil {
		t.Errorf("expected an error for a record with no schema_version")
	}
}

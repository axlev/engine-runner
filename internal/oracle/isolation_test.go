package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// reviewPathPackages are the parts of the engine that run or orchestrate
// reasoners 1-3. None of them may reach the oracle.
var reviewPathPackages = []string{
	"internal/adapters",
	"internal/orchestrator",
	"internal/runner",
	"internal/evaluation",
	"cmd/bench",
}

// TestOracleIsUnreachableFromTheReviewPath is the whole point of this file.
//
// The miner's export contract says the evaluator directory "must never be
// exposed to an engine", and system-design section 6.5 requires the judge to
// be isolated from reasoners 1-3. Until this package existed, that isolation
// held for an accidental reason: nothing read the oracle, so nothing could
// leak it. The guarantee was one `import` statement away from being false and
// nothing would have complained.
//
// This test makes that import fail. It is deliberately a source scan rather
// than a `go list` call so it needs no toolchain network access and cannot be
// satisfied by a stale build cache: if any file under a review-path package
// mentions this package's import path, the test fails and names the file.
//
// If a future design genuinely needs the review path to see oracle data, this
// test is the place that decision gets argued - not the import line.
func TestOracleIsUnreachableFromTheReviewPath(t *testing.T) {
	const oraclePkg = "engine-runner/internal/oracle"

	repoRoot := filepath.Join("..", "..")
	var violations []string

	for _, pkg := range reviewPathPackages {
		root := filepath.Join(repoRoot, pkg)
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("review-path package %s not found; this test's list is stale: %v", pkg, err)
		}
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), oraclePkg) {
				violations = append(violations, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", pkg, err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("the review path must never import %s, but these files reference it: %v\n"+
			"The oracle is the answer key. A reasoner that can read it is not reviewing, it is "+
			"copying, and every result produced afterwards is void.", oraclePkg, violations)
	}
}

// TestNoRealOracleContentInFixtures guards the other direction of the same
// discipline. The fixtures this package tests against must stay synthetic:
// a real correlated-report.json copied in would put the pilot cohort's answer
// key in the repository, where it would be read by anyone doing blind
// analysis and by any future reviewer prompt that globs the tree.
//
// Synthetic fixtures all declare the placeholder repository; a real record
// names a real one.
func TestNoRealOracleContentInFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "fixtures", "oracle")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("no oracle fixtures found; this test would pass vacuously")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		if !strings.Contains(string(data), `"synthetic/repo"`) {
			t.Errorf("%s does not declare the synthetic placeholder repository; "+
				"oracle fixtures must be hand-authored, never copied from a real case", e.Name())
		}
		for _, marker := range []string{"frrouting", "FRRouting"} {
			if strings.Contains(string(data), marker) {
				t.Errorf("%s mentions %q - that looks like real cohort data", e.Name(), marker)
			}
		}
	}
}

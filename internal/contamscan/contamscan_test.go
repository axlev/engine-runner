package contamscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixSHA = "0123456789abcdef0123456789abcdef01234567"

func keysFor(t *testing.T, dir string, k Keys) (Keys, string) {
	t.Helper()
	k.SchemaVersion = KeysSchemaVersion
	b, _ := json.Marshal(k)
	p := filepath.Join(dir, k.CaseID+".json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, sum, err := LoadKeys(p)
	if err != nil {
		t.Fatal(err)
	}
	return loaded, sum
}

func runWith(t *testing.T, dir, caseID string, reviewA string) string {
	t.Helper()
	runDir := filepath.Join(dir, "run-"+caseID)
	if err := os.MkdirAll(filepath.Join(runDir, "stages", "reasoner-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"run_id":"run-` + caseID + `","case_id":"` + caseID + `","protocol_version":"h1-g-v1"}`
	_ = os.WriteFile(filepath.Join(runDir, "run.json"), []byte(manifest), 0o644)
	_ = os.WriteFile(filepath.Join(runDir, "stages", "reasoner-1", "review-a.json"), []byte(reviewA), 0o644)
	return runDir
}

func bundleWith(t *testing.T, dir, diff, metadata string) string {
	t.Helper()
	root := filepath.Join(dir, "bundle")
	_ = os.MkdirAll(filepath.Join(root, "reviewer"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "reviewer", "diff.patch"), []byte(diff), 0o644)
	_ = os.WriteFile(filepath.Join(root, "reviewer", "metadata.json"), []byte(metadata), 0o644)
	return root
}

func kinds(s Scan) string {
	var ks []string
	for _, h := range s.Hits {
		ks = append(ks, h.Kind)
	}
	return strings.Join(ks, ",")
}

func TestCleanReviewIsNotVoided(t *testing.T) {
	dir := t.TempDir()
	k, sum := keysFor(t, dir, Keys{CaseID: "c1", FixingSHAs: []string{fixSHA}, FixingPRNumbers: []int{4242}, CVEIDs: []string{"CVE-2026-0001"}})
	run := runWith(t, dir, "c1", `{"findings":[{"mechanism":"the loop at line 4242 runs one iteration too many","evidence":[{"file":"a.c","start_line":4242}]}]}`)
	s, err := ScanRun(run, k, sum, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Void || len(s.Hits) != 0 {
		t.Errorf("bare number 4242 must not match a PR; hits=%v", s.Hits)
	}
	if s.Rules.ShingleTokens != ShingleTokens || s.Rules.SHAMinPrefix != 7 || s.KeysSHA256 != sum || s.Arm != "h1-g-v1" {
		t.Errorf("scan not self-describing: %+v", s)
	}
}

func TestEachKindVoids(t *testing.T) {
	cases := map[string]struct{ text, kind, keyRef string }{
		"sha prefix of 7":      {`fixed later in 0123456`, "sha", fixSHA},
		"sha full, upper case": {`see ` + strings.ToUpper(fixSHA), "sha", fixSHA},
		"pr hash form":         {`addressed in #4242`, "pr", "4242"},
		"pr word form":         {`this is PR 4242`, "pr", "4242"},
		"pr pull path":         {`github.com/x/y/pull/4242`, "pr", "4242"},
		"keyed cve":            {`this is cve-2026-0001`, "cve", "CVE-2026-0001"},
		"any other cve":        {`resembles CVE-2020-9999`, "cve", "other"},
	}
	for name, c := range cases {
		dir := t.TempDir()
		k, sum := keysFor(t, dir, Keys{CaseID: "c", FixingSHAs: []string{fixSHA}, FixingPRNumbers: []int{4242}, CVEIDs: []string{"CVE-2026-0001"}})
		run := runWith(t, dir, "c", `{"findings":[{"uncertainty":"`+c.text+`"}]}`)
		s, err := ScanRun(run, k, sum, "")
		if err != nil {
			t.Fatal(err)
		}
		if !s.Void || len(s.Hits) != 1 || s.Hits[0].Kind != c.kind || s.Hits[0].KeyRef != c.keyRef {
			t.Errorf("%s: void=%v hits=%+v", name, s.Void, s.Hits)
			continue
		}
		if s.Hits[0].Stage != "reasoner-1" || !strings.Contains(s.Hits[0].JSONPath, "uncertainty") {
			t.Errorf("%s: hit not located: %+v", name, s.Hits[0])
		}
	}
}

func TestSixHexCharsIsNotASHA(t *testing.T) {
	dir := t.TempDir()
	k, sum := keysFor(t, dir, Keys{CaseID: "c", FixingSHAs: []string{fixSHA}})
	run := runWith(t, dir, "c", `{"summary":"prefix 012345 is too short"}`)
	s, _ := ScanRun(run, k, sum, "")
	if s.Void {
		t.Errorf("6-char prefix must not match: %+v", s.Hits)
	}
}

func TestDiscussionShinglesVoidUnlessInTheBundle(t *testing.T) {
	dir := t.TempDir()
	lifted := "the timer is cancelled before the session state is torn down"
	quotedDiff := "if (peer->status == Deleted) return; /* guard added upstream by the caller */"
	citedLine := "static void session_free(struct session *s) { list_delete(&s->timers); XFREE(MTYPE_SESSION, s); }"
	k, sum := keysFor(t, dir, Keys{CaseID: "c", Discussion: []Discussion{{Source: "pr-4242-comment-1",
		Body: "As discussed, " + lifted + ". Also note " + quotedDiff + " and " + citedLine}}})
	// The lifted sentence in prose; the diff line in prose (not an excerpt);
	// the cited file's line in prose too; and the diff line as an excerpt.
	// The two quoted lines are joined with DIFFERENT words here and in the
	// discussion body: with the same connector, an 8-token window bridging
	// the two lines would be shared, be in neither source, and hit - which
	// is correct by the rule, but not what this test is about.
	review := `{"findings":[{"mechanism":"Because ` + lifted + `, the callback fires on freed memory.",
	  "uncertainty":"the guard ` + quotedDiff + `; separately the free path ` + citedLine + `",
	  "evidence":[{"file":"lib/session.c","excerpt":"` + quotedDiff + `"}]}]}`
	run := runWith(t, dir, "c", review)

	// Without a bundle: prose overlaps hit and say they are unexcluded;
	// the excerpt path is never scanned by the discussion rule.
	s, err := ScanRun(run, k, sum, "")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Void || s.ExclusionApplied {
		t.Fatalf("expected unexcluded discussion hits: %+v", s)
	}
	for _, h := range s.Hits {
		if h.Kind != "discussion" || !h.Unexcluded || h.KeyRef != "pr-4242-comment-1" {
			t.Errorf("unexpected hit %+v", h)
		}
		if strings.HasSuffix(h.JSONPath, ".excerpt") {
			t.Errorf("discussion rule scanned an excerpt: %+v", h)
		}
	}
	if len(s.Hits) != 3 {
		t.Errorf("want 3 span hits (lifted, diff-in-prose, cited-in-prose), got %d: %+v", len(s.Hits), s.Hits)
	}

	// With the bundle: the diff line and the cited file's line are
	// excluded; only the lifted sentence remains, as one span.
	bundle := bundleWith(t, dir, "+"+quotedDiff+"\n", `{"commit_messages":["fix: guard"]}`)
	_ = os.MkdirAll(filepath.Join(bundle, "reviewer", "repository", "lib"), 0o755)
	_ = os.WriteFile(filepath.Join(bundle, "reviewer", "repository", "lib", "session.c"), []byte("/* header */\n"+citedLine+"\n"), 0o644)
	s, err = ScanRun(run, k, sum, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Void || !s.ExclusionApplied {
		t.Fatalf("lifted phrase must still void with exclusion: %+v", s)
	}
	if len(s.Hits) != 1 || !strings.Contains(s.Hits[0].JSONPath, "mechanism") || s.Hits[0].Unexcluded {
		t.Fatalf("want exactly the lifted-sentence hit, got %+v", s.Hits)
	}
	if !strings.Contains(s.Hits[0].Matched, "timer is cancelled before the session state is torn down") {
		t.Errorf("span not collapsed to the whole shared phrase: %q", s.Hits[0].Matched)
	}
	if strings.Join(s.Rules.ExclusionSources, ",") != "reviewer/diff.patch,reviewer/metadata.json,reviewer/repository/lib/session.c" {
		t.Errorf("exclusion sources not recorded: %v", s.Rules.ExclusionSources)
	}
	if len(s.Rules.DiscussionSkipsPaths) != 1 {
		t.Errorf("skip rule not recorded: %v", s.Rules.DiscussionSkipsPaths)
	}
}

func TestSHAInAnExcerptStillHits(t *testing.T) {
	dir := t.TempDir()
	k, sum := keysFor(t, dir, Keys{CaseID: "c", FixingSHAs: []string{fixSHA}})
	run := runWith(t, dir, "c", `{"findings":[{"evidence":[{"file":"a.c","excerpt":"/* see 0123456789abcdef */"}]}]}`)
	s, _ := ScanRun(run, k, sum, "")
	if !s.Void || s.Hits[0].Kind != "sha" {
		t.Errorf("sha rule must scan excerpts: %+v", s.Hits)
	}
}

func TestKeysTolerateUnknownFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k.json")
	_ = os.WriteFile(p, []byte(`{"schema_version":"contamination-keys/v1","case_id":"c","fixing_shas":["`+fixSHA+`"],
	  "fixing_commits":[{"sha":"`+fixSHA+`","tier":"strong","source_type":"x","matched_by":"y"}],"future_field":{"a":1}}`), 0o644)
	k, _, err := LoadKeys(p)
	if err != nil || len(k.FixingSHAs) != 1 {
		t.Errorf("unknown top-level keys must be tolerated: %v", err)
	}
}

func TestShortOverlapIsNotAPhrase(t *testing.T) {
	dir := t.TempDir()
	k, sum := keysFor(t, dir, Keys{CaseID: "c", Discussion: []Discussion{{Source: "x", Body: "the null check was missing here"}}})
	run := runWith(t, dir, "c", `{"summary":"the null check was missing here entirely"}`)
	s, _ := ScanRun(run, k, sum, "")
	if s.Void {
		t.Errorf("a %d-token overlap is below the shingle length: %+v", 6, s.Hits)
	}
}

func TestKeysAreValidated(t *testing.T) {
	dir := t.TempDir()
	bad := []string{
		`{"schema_version":"contamination-keys/v2","case_id":"c"}`,
		`{"schema_version":"contamination-keys/v1"}`,
		`{"schema_version":"contamination-keys/v1","case_id":"c","fixing_shas":["abc1234"]}`,
	}
	for i, b := range bad {
		p := filepath.Join(dir, "k"+string(rune('a'+i))+".json")
		_ = os.WriteFile(p, []byte(b), 0o644)
		if _, _, err := LoadKeys(p); err == nil {
			t.Errorf("keys %d should have been refused", i)
		}
	}
}

func TestRunForAnotherCaseIsRefused(t *testing.T) {
	dir := t.TempDir()
	k, sum := keysFor(t, dir, Keys{CaseID: "c1"})
	run := runWith(t, dir, "c2", `{"findings":[]}`)
	if _, err := ScanRun(run, k, sum, ""); err == nil {
		t.Error("keys for c1 must not scan a run of c2")
	}
}

func TestSummaryCountsPerArm(t *testing.T) {
	s := Summarise([]Scan{
		{Arm: "h1-t-v1", Void: true, RunID: "r1", CaseID: "c1", Hits: []Hit{{Kind: "sha"}, {Kind: "cve"}, {Kind: "sha"}}},
		{Arm: "h1-t-v1", Void: false},
		{Arm: "h1-g-v1", Void: false},
	}, []string{"r9"}, "keys")
	if s.Arms["h1-t-v1"].Scanned != 2 || s.Arms["h1-t-v1"].Voided != 1 || s.Arms["h1-g-v1"].Voided != 0 {
		t.Errorf("%+v", s.Arms)
	}
	if len(s.Voided) != 1 || strings.Join(s.Voided[0].Kinds, ",") != "cve,sha" || len(s.Skipped) != 1 {
		t.Errorf("%+v", s)
	}
}

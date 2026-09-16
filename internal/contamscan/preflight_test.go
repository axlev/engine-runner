package contamscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bundleFor(t *testing.T, dir, metadata, patch string) string {
	t.Helper()
	root := filepath.Join(dir, "case-x")
	if err := os.MkdirAll(filepath.Join(root, "reviewer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if metadata != "" {
		_ = os.WriteFile(filepath.Join(root, "reviewer", "metadata.json"), []byte(metadata), 0o644)
	}
	_ = os.WriteFile(filepath.Join(root, "reviewer", "diff.patch"), []byte(patch), 0o644)
	return root
}

func preflightKeys() Keys {
	return Keys{
		SchemaVersion:   KeysSchemaVersion,
		CaseID:          "case-x",
		FixingSHAs:      []string{"0123456789abcdef0123456789abcdef01234567"},
		FixingPRNumbers: []int{4242},
		CVEIDs:          []string{"CVE-2026-0001"},
	}
}

const cleanPatch = `diff --git a/bgpd/bgp_evpn.c b/bgpd/bgp_evpn.c
index 1a2b3c4d5e6f..9f8e7d6c5b4a 100644
--- a/bgpd/bgp_evpn.c
+++ b/bgpd/bgp_evpn.c
@@ -10,6 +10,7 @@ static void drain(void)
 	while (fifo_count(&q)) {
+		if (!entry_matches(e))
+			continue;
 		process(e);
 	}
`

// The whole point: the reviewer-visible text must not carry THIS case's
// fixing identifiers.
func TestPreflightFailsOnTheCasesOwnFixingIdentifiers(t *testing.T) {
	keys := preflightKeys()
	cases := map[string]struct {
		metadata, patch string
		fail            bool
		kind            string
	}{
		"clean": {
			`{"schema_version":"reviewer-metadata/v2","description":"Fixes a drain loop."}`,
			cleanPatch, false, "",
		},
		"fixing sha in the description": {
			`{"schema_version":"reviewer-metadata/v2","description":"See 0123456789a for the follow-up."}`,
			cleanPatch, true, "sha",
		},
		"fixing pr in the description": {
			`{"schema_version":"reviewer-metadata/v2","description":"Superseded by #4242."}`,
			cleanPatch, true, "pr",
		},
		"keyed cve in the description": {
			`{"schema_version":"reviewer-metadata/v2","description":"Tracked as CVE-2026-0001."}`,
			cleanPatch, true, "cve",
		},
		"fixing sha in the diff body": {
			`{"schema_version":"reviewer-metadata/v2"}`,
			cleanPatch + "+\t/* corrected later in 0123456789abcdef */\n", true, "sha",
		},
	}
	for name, c := range cases {
		root := bundleFor(t, t.TempDir(), c.metadata, c.patch)
		p, err := PreflightCase(root, keys, "sum")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.Fail != c.fail {
			t.Errorf("%s: fail=%v want %v (hits %+v)", name, p.Fail, c.fail, p.Hits)
			continue
		}
		if c.fail && p.Hits[0].Kind != c.kind {
			t.Errorf("%s: kind %q want %q", name, p.Hits[0].Kind, c.kind)
		}
	}
}

// Git's own metadata lines carry blob ids, present by construction. Scanning
// them would fail bundles on coincidence while saying nothing about a leak.
func TestPreflightIgnoresDiffHeaderHexButNotTheBody(t *testing.T) {
	keys := preflightKeys()
	// A blob id that happens to prefix-match the fixing sha.
	header := `diff --git a/x.c b/x.c
index 0123456789abcdef0123456789abcdef01234567..9f8e7d6c 100644
--- a/x.c
+++ b/x.c
@@ -1 +1 @@
-old
+new
`
	root := bundleFor(t, t.TempDir(), `{"schema_version":"reviewer-metadata/v2"}`, header)
	p, err := PreflightCase(root, keys, "sum")
	if err != nil {
		t.Fatal(err)
	}
	if p.Fail {
		t.Errorf("a blob id in an index line must not fail the case: %+v", p.Hits)
	}
	if len(p.Sources) != 2 || !strings.Contains(p.Sources[1], "excluded") {
		t.Errorf("the report must say headers were excluded: %v", p.Sources)
	}

	// The same hex in the body IS a hit.
	body := header + "+/* see 0123456789abcdef0123456789abcdef01234567 */\n"
	root2 := bundleFor(t, t.TempDir(), `{"schema_version":"reviewer-metadata/v2"}`, body)
	p2, _ := PreflightCase(root2, keys, "sum")
	if !p2.Fail {
		t.Error("a fixing sha in the diff body must fail")
	}
}

// Another case's identifiers are not this case's leak.
func TestPreflightIsPerCase(t *testing.T) {
	keys := preflightKeys()
	root := bundleFor(t, t.TempDir(),
		`{"schema_version":"reviewer-metadata/v2","description":"Unrelated to abcdef1234567890 or #9999."}`,
		cleanPatch)
	p, err := PreflightCase(root, keys, "sum")
	if err != nil {
		t.Fatal(err)
	}
	if p.Fail {
		t.Errorf("identifiers that are not this case's fixing ids must not fail it: %+v", p.Hits)
	}
}

func TestPreflightSummaryDoesNotClearUncheckedCases(t *testing.T) {
	s := SummarisePreflight([]Preflight{
		{CaseID: "a", Fail: true},
		{CaseID: "b", Fail: false},
	}, []string{"c"})
	if len(s.Failed) != 1 || s.Failed[0] != "a" || s.Scanned != 2 {
		t.Errorf("%+v", s)
	}
	if len(s.Skipped) != 1 || !strings.Contains(s.Note, "NOT cleared") {
		t.Errorf("an unchecked case must not read as clean: %+v", s)
	}
}

func TestPreflightRecordsRulesAndIsSerialisable(t *testing.T) {
	root := bundleFor(t, t.TempDir(), `{"schema_version":"reviewer-metadata/v2"}`, cleanPatch)
	p, err := PreflightCase(root, preflightKeys(), "sum")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rules.SHAMinPrefix != MinSHAPrefix || p.KeysSHA256 != "sum" {
		t.Errorf("rules not recorded: %+v", p.Rules)
	}
	if p.Rules.ShingleTokens != 0 {
		t.Errorf("the discussion rule does not apply to inputs and must not be claimed: %+v", p.Rules)
	}
	if _, err := json.Marshal(p); err != nil {
		t.Fatal(err)
	}
}

package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

func TestBuildClaudeArgsAPIKeyIncludesBare(t *testing.T) {
	req := adapters.RunRequest{Model: "sonnet", ReasoningLevel: "high", Budget: adapters.Budget{MaxCostUSD: 0.5}}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialAPIKey, Value: "sk-test"})

	if !containsAdjacent(args, "--bare") {
		t.Errorf("expected --bare for API key auth, got %v", args)
	}
}

func TestBuildClaudeArgsOAuthTokenOmitsBare(t *testing.T) {
	req := adapters.RunRequest{Model: "sonnet"}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialOAuthToken, Value: "oauth-test"})

	if containsAdjacent(args, "--bare") {
		t.Errorf("did not expect --bare for OAuth token auth (bare mode never reads OAuth), got %v", args)
	}
}

func TestBuildClaudeArgsIncludesOutputFormatAndToolRestriction(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey})
	if !containsSubsequence(args, []string{"--output-format", "json"}) {
		t.Errorf("expected --output-format json, got %v", args)
	}
	if !containsSubsequence(args, []string{"--tools", "Read"}) {
		t.Errorf("expected --tools Read, got %v", args)
	}
	if !containsAdjacent(args, "-p") {
		t.Errorf("expected -p (print mode), got %v", args)
	}
}

// The tool set is an experimental variable, not an adapter constant: a
// reviewer that can search reaches conclusions one that can only open named
// paths cannot. These two tests are what stop it silently reverting to a
// hardcoded grant that no fingerprint would record.
func TestBuildClaudeArgsUsesRequestedToolSet(t *testing.T) {
	req := adapters.RunRequest{Tools: []string{"Read", "Grep", "Glob"}}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialAPIKey})

	if !containsSubsequence(args, []string{"--tools", "Read,Grep,Glob"}) {
		t.Errorf("expected the request's tool set to reach --tools, got %v", args)
	}
	if containsSubsequence(args, []string{"--tools", "Read"}) {
		t.Errorf("expected the hardcoded narrow grant to be gone, got %v", args)
	}
}

func TestBuildClaudeArgsFallsBackToNarrowestToolSet(t *testing.T) {
	// A hand-built request that never went through LoadAgentSet must not
	// inherit the CLI's own default, which would widen capability silently.
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey})
	if !containsSubsequence(args, []string{"--tools", "Read"}) {
		t.Errorf("expected a tool-less request to fall back to Read, got %v", args)
	}
}

func TestBuildClaudeArgsModelAndEffortAndBudget(t *testing.T) {
	req := adapters.RunRequest{Model: "claude-opus-5", ReasoningLevel: "xhigh", Budget: adapters.Budget{MaxCostUSD: 1.25}}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialAPIKey})

	if !containsSubsequence(args, []string{"--model", "claude-opus-5"}) {
		t.Errorf("expected --model claude-opus-5, got %v", args)
	}
	if !containsSubsequence(args, []string{"--effort", "xhigh"}) {
		t.Errorf("expected --effort xhigh, got %v", args)
	}
	if !containsSubsequence(args, []string{"--max-budget-usd", "1.25"}) {
		t.Errorf("expected --max-budget-usd 1.25, got %v", args)
	}
}

func TestBuildClaudeArgsOmitsUnsetOptionalFlags(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey})
	for _, flag := range []string{"--model", "--effort", "--max-budget-usd"} {
		if containsAdjacent(args, flag) {
			t.Errorf("did not expect %s when unset, got %v", flag, args)
		}
	}
}

// The prompt must NOT reach argv. A prompt large enough to exceed
// MAX_ARG_STRLEN (128 KiB) failed the whole invocation with "argument list
// too long" before any model call, so a case with a big diff could not run
// at all. It is piped on stdin instead; this pins that it stays off argv.
func TestBuildClaudeArgsNeverPutsThePromptInArgv(t *testing.T) {
	const prompt = "the actual prompt text"
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey})
	for _, a := range args {
		if strings.Contains(a, prompt) {
			t.Fatalf("prompt text reached argv: %v", args)
		}
	}
	// Nothing prompt-sized belongs in argv at any size.
	for _, a := range args {
		if len(a) > 4096 {
			t.Fatalf("an argv element is %d bytes; the prompt must travel on stdin", len(a))
		}
	}
}

// containsAdjacent and containsSubsequence mirror the helpers in
// internal/runner's container_test.go; duplicated locally to keep this
// package's tests independent of runner's test-only helpers.
func containsAdjacent(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

func containsSubsequence(got, want []string) bool {
	if len(want) > len(got) {
		return false
	}
	for i := 0; i+len(want) <= len(got); i++ {
		match := true
		for j := range want {
			if got[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestVendorJSONSchemaStripsOnlyWhatBlocksTheCLI pins the exact reason the
// first --json-schema attempt failed: the CLI rejected our schema outright
// with "no schema with key or ref https://json-schema.org/draft/2020-12/
// schema". $schema and $id must go; $defs and $ref must survive, because
// every review schema describes its findings through them.
func TestVendorJSONSchemaStripsOnlyWhatBlocksTheCLI(t *testing.T) {
	raw, err := os.ReadFile("../../../schemas/review-a.schema.json")
	if err != nil {
		t.Fatalf("reading the real schema: %v", err)
	}

	got := vendorJSONSchema(raw)
	if got == "" {
		t.Fatalf("the real review-a schema produced no vendor schema")
	}

	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	for _, gone := range metaKeysClaudeCannotResolve {
		if _, present := doc[gone]; present {
			t.Errorf("%s survived; the CLI rejects the whole schema over it", gone)
		}
	}
	// The parts that carry the actual contract must be intact.
	if _, ok := doc["$defs"]; !ok {
		t.Errorf("$defs was stripped; findings and evidence_ref are defined there")
	}
	if _, ok := doc["properties"]; !ok {
		t.Errorf("properties was stripped; the schema would constrain nothing")
	}
	if !strings.Contains(got, "$ref") {
		t.Errorf("$ref did not survive; findings reference $defs/finding")
	}
}

// A missing or unparseable schema must degrade to "no --json-schema flag",
// never to a failed stage: the caller validates the output either way, so
// the vendor's help is an optimisation, not a dependency.
func TestVendorJSONSchemaDegradesRatherThanFailing(t *testing.T) {
	for name, input := range map[string][]byte{
		"nil":        nil,
		"empty":      {},
		"notJSON":    []byte("Based on my analysis..."),
		"jsonbutnot": []byte("[1,2,3]"),
	} {
		if got := vendorJSONSchema(input); got != "" {
			t.Errorf("%s: got %q, want \"\" so the flag is simply omitted", name, got)
		}
	}
}

// The flag must only appear when a schema was actually supplied.
func TestJSONSchemaFlagOmittedWithoutASchema(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey})
	for _, a := range args {
		if a == "--json-schema" {
			t.Fatalf("--json-schema passed with no schema available: %v", args)
		}
	}
}

// TestRepositoryContentCannotSteerTheReviewer pins the property, not the flag:
// under EITHER credential kind, the invocation must disable CLAUDE.md
// discovery so a file inside reviewer/repository/ cannot be read as
// instructions from outside the frozen protocol.
//
// The two kinds reach it by different flags because --bare sets apiKeyHelper
// and cannot be used with a token. A future refactor that drops one of them
// reopens the hole for that credential kind only, which is exactly the sort of
// asymmetry that goes unnoticed.
func TestRepositoryContentCannotSteerTheReviewer(t *testing.T) {
	for _, tc := range []struct {
		kind Credentials
		want string
	}{
		{Credentials{Kind: CredentialAPIKey}, "--bare"},
		{Credentials{Kind: CredentialOAuthToken}, "--safe-mode"},
	} {
		args := buildClaudeArgs(adapters.RunRequest{}, tc.kind)
		found := false
		for _, a := range args {
			if a == tc.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s invocation is missing %s, so CLAUDE.md discovery is live: %v",
				tc.kind.Kind, tc.want, args)
		}
	}
}

// The two flags are mutually exclusive: --bare forces an API key, so passing
// both would make an OAuth run fail at authentication.
func TestBareAndSafeModeAreNeverBothPassed(t *testing.T) {
	for _, kind := range []CredentialKind{CredentialAPIKey, CredentialOAuthToken} {
		args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: kind})
		var bare, safe bool
		for _, a := range args {
			bare = bare || a == "--bare"
			safe = safe || a == "--safe-mode"
		}
		if bare && safe {
			t.Errorf("%s: both --bare and --safe-mode passed; --bare forces an API key", kind)
		}
	}
}

// The sealed result must record exactly the flags the invocation passed:
// one definition, so an artifact cannot claim a flag the CLI never got.
func TestSafetyFlagsMatchTheInvocation(t *testing.T) {
	for _, kind := range []CredentialKind{CredentialAPIKey, CredentialOAuthToken} {
		args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: kind})
		flags := safetyFlagsFor(kind)
		if len(flags) != 1 {
			t.Fatalf("%s: expected one safety flag, got %v", kind, flags)
		}
		if !containsAdjacent(args, flags[0]) {
			t.Errorf("%s: recorded %v but the invocation was %v", kind, flags, args)
		}
	}
	if f := safetyFlagsFor(CredentialKind("none")); f != nil {
		t.Errorf("unknown kind must record no flags, got %v", f)
	}
}

// A11's probes must run with no tools at all. An unset grant still means
// Read; only NoTools means none, and it reaches the CLI as --tools "".
func TestNoToolsWithholdsEveryTool(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{NoTools: true}, Credentials{Kind: CredentialOAuthToken})
	for i, a := range args {
		if a == "--tools" {
			if i+1 >= len(args) || args[i+1] != "" {
				t.Fatalf("expected --tools with an empty value, got %v", args)
			}
			return
		}
	}
	t.Fatalf("no --tools flag in %v", args)
}

func TestUnsetToolsStillMeansRead(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialOAuthToken})
	for i, a := range args {
		if a == "--tools" && i+1 < len(args) && args[i+1] == "Read" {
			return
		}
	}
	t.Fatalf("expected --tools Read for an unset grant, got %v", args)
}

// The vendor rejects oneOf/allOf/anyOf at the top level of a tool
// input_schema, which failed every arm run at reasoner-1: h1-review-a
// carries a top-level allOf and both arms use that schema. Structure must
// survive; the constraint must not be sent.
func TestVendorJSONSchemaDropsTopLevelCombinators(t *testing.T) {
	raw := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"required": ["findings"],
		"properties": {"findings": {"type": "array"}},
		"allOf": [{"if": {"properties": {"findings": {"maxItems": 0}}},
		           "then": {"required": ["empty_reason"]}}]
	}`)
	got := vendorJSONSchema(raw)
	if got == "" {
		t.Fatal("schema was dropped entirely")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf", "if", "then", "else"} {
		if _, bad := doc[k]; bad {
			t.Errorf("%q survived at the top level; the API rejects it", k)
		}
	}
	// The shape the model must produce has to survive the stripping.
	if doc["type"] != "object" {
		t.Error("type was lost")
	}
	if _, ok := doc["properties"]; !ok {
		t.Error("properties were lost")
	}
	if _, ok := doc["required"]; !ok {
		t.Error("required was lost")
	}
}

// The real schema that failed must now be sendable.
func TestRealReviewASchemaIsSendable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "h1-review-a.schema.json"))
	if err != nil {
		t.Skipf("schema not readable: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(vendorJSONSchema(raw)), &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		if _, bad := doc[k]; bad {
			t.Errorf("h1-review-a still sends %q at the top level", k)
		}
	}
}

// Every schema this repository ships must survive the vendor projection
// with no combinator left at its root. The failure this guards was found by
// a paid arm run, not by a test: -dry-run uses the fixture adapter, which
// never projects a schema and never calls the API, so a schema the vendor
// cannot accept passes every rehearsal and fails only when it costs money.
// This closes that gap for the whole schemas/ directory at once, so a new
// or edited schema cannot reintroduce it silently.
func TestEveryShippedSchemaProjectsWithoutRootCombinators(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "schemas")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var checked int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		projected := vendorJSONSchema(raw)
		if projected == "" {
			// Nothing is sent for this schema, so nothing can be rejected.
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(projected), &doc); err != nil {
			t.Errorf("%s: projection is not valid JSON: %v", e.Name(), err)
			continue
		}
		checked++
		for _, k := range []string{"allOf", "anyOf", "oneOf"} {
			if _, bad := doc[k]; bad {
				t.Errorf("%s projects with %q at the top level; the API rejects that and every run using this schema fails at the first stage", e.Name(), k)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no schemas were projected; the test is not exercising anything")
	}
}

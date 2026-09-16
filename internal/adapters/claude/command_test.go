package claude

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

func TestBuildClaudeArgsAPIKeyIncludesBare(t *testing.T) {
	req := adapters.RunRequest{Model: "sonnet", ReasoningLevel: "high", Budget: adapters.Budget{MaxCostUSD: 0.5}}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialAPIKey, Value: "sk-test"}, "prompt text")

	if !containsAdjacent(args, "--bare") {
		t.Errorf("expected --bare for API key auth, got %v", args)
	}
}

func TestBuildClaudeArgsOAuthTokenOmitsBare(t *testing.T) {
	req := adapters.RunRequest{Model: "sonnet"}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialOAuthToken, Value: "oauth-test"}, "prompt text")

	if containsAdjacent(args, "--bare") {
		t.Errorf("did not expect --bare for OAuth token auth (bare mode never reads OAuth), got %v", args)
	}
}

func TestBuildClaudeArgsIncludesOutputFormatAndToolRestriction(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey}, "prompt")
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
	args := buildClaudeArgs(req, Credentials{Kind: CredentialAPIKey}, "prompt")

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
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey}, "prompt")
	if !containsSubsequence(args, []string{"--tools", "Read"}) {
		t.Errorf("expected a tool-less request to fall back to Read, got %v", args)
	}
}

func TestBuildClaudeArgsModelAndEffortAndBudget(t *testing.T) {
	req := adapters.RunRequest{Model: "claude-opus-5", ReasoningLevel: "xhigh", Budget: adapters.Budget{MaxCostUSD: 1.25}}
	args := buildClaudeArgs(req, Credentials{Kind: CredentialAPIKey}, "prompt")

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
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey}, "prompt")
	for _, flag := range []string{"--model", "--effort", "--max-budget-usd"} {
		if containsAdjacent(args, flag) {
			t.Errorf("did not expect %s when unset, got %v", flag, args)
		}
	}
}

func TestBuildClaudeArgsPromptIsFinalArgument(t *testing.T) {
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey}, "the actual prompt text")
	if args[len(args)-1] != "the actual prompt text" {
		t.Errorf("last arg = %q, want the prompt text", args[len(args)-1])
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
	args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: CredentialAPIKey}, "prompt")
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
		args := buildClaudeArgs(adapters.RunRequest{}, tc.kind, "prompt")
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
		args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: kind}, "prompt")
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
		args := buildClaudeArgs(adapters.RunRequest{}, Credentials{Kind: kind}, "p")
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

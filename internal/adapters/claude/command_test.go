package claude

import (
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

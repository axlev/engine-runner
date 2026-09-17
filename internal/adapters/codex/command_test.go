package codex

import (
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

func TestBuildCodexArgsIncludesIsolationFlags(t *testing.T) {
	args := buildCodexArgs(adapters.RunRequest{}, "/workspace/output/reasoner-1.json")
	for _, want := range [][]string{
		{"--skip-git-repo-check"},
		{"--ephemeral"},
		{"--sandbox", "read-only"},
		{"-a", "never"},
		{"-o", "/workspace/output/reasoner-1.json"},
	} {
		if !containsSubsequence(args, want) {
			t.Errorf("expected %v in args, got %v", want, args)
		}
	}
	if args[0] != "exec" {
		t.Errorf("args[0] = %q, want \"exec\"", args[0])
	}
}

func TestBuildCodexArgsModelAndEffort(t *testing.T) {
	req := adapters.RunRequest{Model: "gpt-5.6-sol", ReasoningLevel: "high"}
	args := buildCodexArgs(req, "/out.json")

	if !containsSubsequence(args, []string{"-m", "gpt-5.6-sol"}) {
		t.Errorf("expected -m gpt-5.6-sol, got %v", args)
	}
	if !containsSubsequence(args, []string{"-c", "model_reasoning_effort=high"}) {
		t.Errorf("expected -c model_reasoning_effort=high, got %v", args)
	}
}

func TestBuildCodexArgsOmitsUnsetOptionalFlags(t *testing.T) {
	args := buildCodexArgs(adapters.RunRequest{}, "/out.json")
	if containsAdjacent(args, "-m") {
		t.Errorf("did not expect -m when Model is unset, got %v", args)
	}
	if containsAdjacent(args, "-c") {
		t.Errorf("did not expect -c when ReasoningLevel is unset, got %v", args)
	}
}

func TestBuildCodexArgsPromptIsFinalArgument(t *testing.T) {
	const prompt = "the actual prompt text"
	args := buildCodexArgs(adapters.RunRequest{}, "/out.json")
	for _, a := range args {
		if strings.Contains(a, prompt) {
			t.Fatalf("prompt text reached argv: %v", args)
		}
	}
}

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

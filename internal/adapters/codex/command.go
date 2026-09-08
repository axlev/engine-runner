package codex

import "github.com/axlev/engine-runner/internal/adapters"

// buildCodexArgs constructs the exact `codex exec` argument list for one
// stage invocation. Every flag here was verified directly against a real
// `codex exec --help` (codex-cli 0.153.2) in this session.
//
// outputPath is a container-internal path (e.g. "/workspace/output/
// reasoner-1.json") that codex writes its final response text to directly
// via -o/--output-last-message - unlike the claude package, there is no
// envelope to unwrap: whatever text codex writes there becomes the stage's
// output file verbatim.
func buildCodexArgs(req adapters.RunRequest, promptText, outputPath string) []string {
	args := []string{
		"exec",
		// Our mounted workspace is a plain copied snapshot, not a git
		// checkout - codex must not refuse to run over that.
		"--skip-git-repo-check",
		// No session file persistence: matches section 9's "no reuse of
		// vendor conversation IDs across cases, stages, or benchmark
		// variants."
		"--ephemeral",
		// The model may read mounted files via shell commands but must
		// never be allowed to write anywhere; -o writing the final
		// message is done by the codex process itself, not a
		// model-invoked, sandboxed tool call, so it is unaffected by this.
		"--sandbox", "read-only",
		// Non-interactive: there is no human present to approve
		// anything, and a policy other than "never" could hang forever
		// waiting for an approval that will never come.
		"-a", "never",
		// Diagnostic event stream. Field-level parsing of this JSONL
		// stream (e.g. for token usage/cost) is deliberately not
		// implemented - see codex.go - so this is kept only for a human
		// to inspect if something goes wrong, not machine-parsed here.
		"--json",
		"-o", outputPath,
	}

	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	if req.ReasoningLevel != "" {
		// Verified against this host's own ~/.codex/config.toml, which
		// has model_reasoning_effort set as a plain config key - -c
		// applies the same override non-interactively.
		args = append(args, "-c", "model_reasoning_effort="+req.ReasoningLevel)
	}

	args = append(args, promptText)
	return args
}

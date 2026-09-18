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
func buildCodexArgs(req adapters.RunRequest, outputPath string) []string {
	args := []string{
		"exec",
		// Our mounted workspace is a plain copied snapshot, not a git
		// checkout - codex must not refuse to run over that.
		"--skip-git-repo-check",
		// No session file persistence: matches section 9's "no reuse of
		// vendor conversation IDs across cases, stages, or benchmark
		// variants."
		"--ephemeral",
		// Codex sandboxes its own tool calls with bubblewrap, which
		// cannot create a user namespace inside our container: every
		// file read fails with "bwrap: No permissions to create new
		// namespace" and the run still exits zero, so the model answers
		// from the prompt alone and the result looks like a considered
		// one. Verified: four verifications returned confident
		// INCONCLUSIVE verdicts about a repository never read.
		//
		// This flag is what codex documents for that case - "intended
		// solely for running in environments that are externally
		// sandboxed" - and that is what the container is. The isolation
		// the read-only sandbox was providing is provided by the mounts
		// instead, which is where it belongs: input is bind-mounted
		// read-only so a write cannot reach the bundle, output is the
		// only writable mount, the process is uid 10001, and the
		// container is --rm. Loosening the OUTER container to let the
		// inner sandbox run would have been the worse trade.
		"--dangerously-bypass-approvals-and-sandbox",
		// No approval flag: `-a` does not exist in codex-cli 0.153.2 and
		// the invocation fails at argument parsing with "unexpected
		// argument '-a' found". `codex exec` already defaults to
		// approval: never - verified by reading the run header of a real
		// containerised call, which printed "approval: never" with no
		// such flag passed. Non-interactive is what we need and what we
		// get; asserting it with a flag that does not parse got us
		// neither.
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

	// The prompt is NOT passed as an argument; it is piped on stdin. argv
	// is bounded by MAX_ARG_STRLEN (128 KiB on Linux) and a real case diff
	// can exceed it. `codex exec` documents that when no PROMPT argument is
	// given, "instructions are read from stdin" - the same contract the
	// claude adapter relies on.
	return args
}

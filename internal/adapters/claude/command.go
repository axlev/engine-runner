package claude

import (
	"strconv"

	"github.com/axlev/engine-runner/internal/adapters"
)

// buildClaudeArgs constructs the exact `claude` CLI argument list for one
// stage invocation. Every flag here was verified directly against a real
// `claude --help` and a real (unauthenticated) invocation in this session -
// not taken on faith from documentation summaries, which turned out to
// include at least two flags (--max-turns, --timeout) that do not exist in
// the real CLI.
//
// promptText is the stage prompt's file content, passed as the trailing
// positional argument rather than a shell "$(cat ...)" substitution, since
// exec.Command never invokes a shell - this also sidesteps any shell
// quoting/injection concern entirely.
func buildClaudeArgs(req adapters.RunRequest, creds Credentials, promptText string) []string {
	var args []string

	// --bare only ever reads ANTHROPIC_API_KEY (confirmed via `claude
	// --help`: "OAuth and keychain are never read" in bare mode). An
	// OAuth-token-authenticated run must skip --bare entirely and run
	// with Claude Code's normal defaults (hooks, LSP, CLAUDE.md discovery,
	// etc. all active) - a real reproducibility gap between the two
	// credential kinds that engine-runner should record, not hide.
	if creds.Kind == CredentialAPIKey {
		args = append(args, "--bare")
	}

	args = append(args, "-p", "--output-format", "json")

	// Replace the tool set entirely rather than layering an allow-list on
	// top of an unknown default (--tools, not --allowed-tools): the model
	// may read the mounted reference files and nothing else.
	args = append(args, "--tools", "Read")

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.ReasoningLevel != "" {
		args = append(args, "--effort", req.ReasoningLevel)
	}
	if req.Budget.MaxCostUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(req.Budget.MaxCostUSD, 'f', -1, 64))
	}

	// The prompt is always the final positional argument.
	args = append(args, promptText)
	return args
}

package claude

import (
	"encoding/json"
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

	// Hand the stage's output contract to the vendor so the model emits the
	// document directly instead of a paragraph of preamble wrapped around a
	// ```json fence. That wrapper is normal model behaviour - the pilot-v1
	// prompts already ask for content only, and a real run produced prose
	// twice, burning both retry attempts on formatting rather than
	// substance.
	//
	// This never replaces the caller's validation, which still runs against
	// the full schema. It only removes a failure mode that measures
	// formatting compliance instead of review quality.
	if schema := vendorJSONSchema(req.OutputSchemaJSON); schema != "" {
		args = append(args, "--json-schema", schema)
	}

	// The prompt is always the final positional argument.
	args = append(args, promptText)
	return args
}

// metaKeysClaudeCannotResolve are dropped before handing a schema to
// --json-schema. $schema declares the draft dialect and the CLI rejects the
// whole schema when it cannot resolve that URI ("no schema with key or ref
// https://json-schema.org/draft/2020-12/schema"), which is how this was
// found; $id is an identifier with the same resolution problem. title is
// dropped alongside them as pure documentation.
//
// $defs and $ref are deliberately NOT touched - verified working, and our
// schemas rely on them for finding/evidence_ref.
var metaKeysClaudeCannotResolve = []string{"$schema", "$id", "title"}

// vendorJSONSchema converts one of our schemas into the form claude's
// --json-schema accepts, returning "" when there is nothing usable to send.
// A schema that will not marshal is skipped rather than fatal: the stage
// should still run and still be validated by the caller, just without the
// vendor's help.
func vendorJSONSchema(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	for _, k := range metaKeysClaudeCannotResolve {
		delete(doc, k)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	return string(out)
}

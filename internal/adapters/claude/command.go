package claude

import (
	"encoding/json"
	"strconv"
	"strings"

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
// safetyFlagsFor returns the customization-disabling flags for a credential
// kind. It is the single definition of that mapping: the invocation passes
// exactly what the sealed result records, so the artifact cannot claim a
// flag the CLI never received.
func safetyFlagsFor(kind CredentialKind) []string {
	switch kind {
	case CredentialAPIKey:
		return []string{"--bare"}
	case CredentialOAuthToken:
		return []string{"--safe-mode"}
	}
	return nil
}

func buildClaudeArgs(req adapters.RunRequest, creds Credentials) []string {
	var args []string

	// Both credential kinds must run with repository content unable to
	// steer the reviewer. They get there by different flags, because
	// --bare sets apiKeyHelper and so cannot be used with a token:
	//
	//   api_key      --bare       skips hooks, LSP, plugin sync, auto-memory
	//                             and CLAUDE.md auto-discovery. "OAuth and
	//                             keychain are never read" in this mode.
	//   oauth_token  --safe-mode  disables all customizations - CLAUDE.md,
	//                             skills, plugins, hooks, MCP servers,
	//                             custom commands and agents.
	//
	// This closes a gap this comment used to merely record: an OAuth run
	// previously executed with CLAUDE.md auto-discovery ACTIVE, so a
	// CLAUDE.md anywhere in reviewer/repository/ would have been read as
	// instructions - from inside the bundle, entirely outside the frozen
	// protocol. Harmless while every snapshot was seven synthetic files;
	// not harmless now that a snapshot is a real 7,600-file source tree.
	//
	// Deliberately NOT solved by rejecting bundles that carry a CLAUDE.md.
	// Real repositories legitimately have one, so a boundary rule would be
	// a proxy that blocks admissible cases - the same over-broad shape as
	// the blanket symlink rejection that had to be corrected. The bundle is
	// not the problem; reading it as instructions is, and that is fixed
	// here.
	//
	// Both flags verified against the real CLI: --safe-mode succeeds under
	// an OAuth token, where --bare cannot.
	args = append(args, safetyFlagsFor(creds.Kind)...)

	args = append(args, "-p", "--output-format", "json")

	// Replace the tool set entirely rather than layering an allow-list on
	// top of an unknown default (--tools, not --allowed-tools), so the
	// grant is exactly what the arm declared and nothing inherited.
	//
	// The set comes from the arm config via the request. It used to be
	// hardcoded "Read" here, which meant reviewer capability was invisible
	// to the fingerprint: a run with search and a run without produced
	// indistinguishable sealed records. The orchestrator has already
	// resolved the arm's default, so an empty slice here means a caller
	// built a RunRequest by hand rather than through LoadAgentSet - fall
	// back to the narrowest set rather than inheriting the CLI's default,
	// which would silently widen capability.
	// --tools "" is the CLI's documented way to disable every tool; an
	// unset grant still falls back to the narrowest set rather than the
	// CLI's default, which would silently widen capability.
	if req.NoTools {
		args = append(args, "--tools", "")
	} else {
		tools := req.Tools
		if len(tools) == 0 {
			tools = []string{"Read"}
		}
		args = append(args, "--tools", strings.Join(tools, ","))
	}

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

	// The prompt is NOT passed as an argument. argv is bounded by
	// MAX_ARG_STRLEN (128 KiB on Linux) and a real case diff can exceed it,
	// which failed the whole invocation with "argument list too long"
	// before any model call. `claude -p` reads the prompt from stdin when
	// no positional prompt is given (verified against the real CLI), so the
	// adapter pipes it there instead. Same bytes, no limit.
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

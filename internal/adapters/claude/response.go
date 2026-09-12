package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

// response is `claude --output-format json`'s envelope shape. Every field
// here was confirmed by actually invoking the real CLI in this session
// (an unauthenticated call, which still produces a fully-formed envelope
// with is_error: true) - not guessed from documentation. Only the fields
// engine-runner actually needs are modeled; the real envelope carries
// several more (subagent_stats, permission_denials, cache accounting, ...)
// that are irrelevant here.
type response struct {
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	SessionID    string  `json:"session_id"`
	NumTurns     int     `json:"num_turns"`

	// Subtype and TerminalReason are the CLI's machine-readable account of
	// WHY a run ended, and they are the only account available when the
	// run was killed rather than answered.
	//
	// The comment on parseResponse used to claim Result always carries a
	// human-readable message when IsError is true. That holds for an auth
	// failure but not for a budget kill, where the envelope is
	// `{"is_error":true, "subtype":"error_max_budget_usd",
	// "terminal_reason":"budget_exhausted", "result":null}` - verified by
	// forcing one against the pinned image. Because only Result was
	// modeled, every budget-killed stage in the narrow opus arm recorded
	// its failure as the string "claude: " and nothing else, so the sealed
	// record could not say why the run died. Two cases were affected and
	// one of them, opus-de6d5d30, retried into a sealed `completed` result
	// that looked indistinguishable from a clean run.
	Subtype        string `json:"subtype"`
	TerminalReason string `json:"terminal_reason"`

	// The CLI's own timing. Kept because the split is what our two
	// timestamps cannot give: DurationAPIMS is time spent in API calls,
	// DurationMS is everything the CLI did. Subtracting them from the
	// orchestrator's wall clock separates model time from engine overhead -
	// which is the question a 7,600-file snapshot raises, since it cannot
	// be answered by a single elapsed figure.
	DurationMS    int `json:"duration_ms"`
	DurationAPIMS int `json:"duration_api_ms"`
	Usage         struct {
		// input_tokens counts ONLY the uncached remainder. With prompt
		// caching on - which Claude Code does by default - almost all of a
		// stage's input arrives as cache reads or cache writes, so this
		// field alone is wildly misleading: a real run reported
		// input_tokens=10 against cache_read=13615 and cache_creation=6799.
		// Budgeting on input_tokens alone made max_input_tokens dead, since
		// 10 never approaches a 120000 bound. TotalInputTokens sums all
		// three, which is what the bound is actually about.
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`

		// output_tokens covers everything the model generated across the
		// whole session - every tool-using turn and its thinking - not just
		// the final review document. A 1.5KB review can legitimately report
		// 10k output tokens.
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// parseResponse decodes one `claude --output-format json` stdout payload.
// When IsError is true the stage produced no review, and callers must check
// IsError before treating Result as structured output. Result MAY carry a
// human-readable message (e.g. "Not logged in · Please run /login") but is
// null for at least the budget-kill case, so use errorMessage rather than
// Result to describe a failure.
func parseResponse(stdout []byte) (response, error) {
	var r response
	if err := json.Unmarshal(stdout, &r); err != nil {
		return response{}, fmt.Errorf("claude: parsing response JSON: %w", err)
	}
	return r, nil
}

// errorMessage renders the best available account of why a run failed,
// preferring the vendor's own prose and falling back to its machine-readable
// reason codes plus the spend that triggered the stop.
//
// The cost is included because for a budget kill it is the whole diagnosis,
// and because max_cost_usd is not a hard ceiling - a probe against the
// pinned image recorded $0.005638 against a $0.002 cap - so the recorded
// figure legitimately exceeds the bound and a reader needs to see by how
// much rather than infer it.
func (r response) errorMessage() string {
	if msg := strings.TrimSpace(r.Result); msg != "" {
		return msg
	}
	var parts []string
	if r.Subtype != "" {
		parts = append(parts, r.Subtype)
	}
	if r.TerminalReason != "" && r.TerminalReason != r.Subtype {
		parts = append(parts, r.TerminalReason)
	}
	if len(parts) == 0 {
		// The CLI reported an error and named no reason for it. Say that,
		// rather than returning "" and producing a bare "claude: " again.
		parts = append(parts, "vendor reported an error with no subtype or terminal_reason")
	}
	return fmt.Sprintf("%s (cost $%.4f, %d turns)", strings.Join(parts, "/"), r.TotalCostUSD, r.NumTurns)
}

// TotalInputTokens is every input token the stage was billed for: the
// uncached remainder plus cache writes plus cache reads. Cached input is
// cheaper, not free, and it is still context the model consumed - so a
// bound on "how much input did this stage take" must count all of it.
func (r response) TotalInputTokens() int {
	return r.Usage.InputTokens +
		r.Usage.CacheCreationInputTokens +
		r.Usage.CacheReadInputTokens
}

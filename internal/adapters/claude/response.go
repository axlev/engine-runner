package claude

import (
	"encoding/json"
	"fmt"
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
// When IsError is true, Result carries a human-readable error message
// (e.g. "Not logged in · Please run /login") rather than review
// content - callers must check IsError before treating Result as the
// stage's structured output.
func parseResponse(stdout []byte) (response, error) {
	var r response
	if err := json.Unmarshal(stdout, &r); err != nil {
		return response{}, fmt.Errorf("claude: parsing response JSON: %w", err)
	}
	return r, nil
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

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
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
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

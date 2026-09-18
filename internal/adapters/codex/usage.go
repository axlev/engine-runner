package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
)

// parseUsage pulls token counts out of `codex exec --json`'s event stream.
//
// The stream is JSONL and the counts arrive on the final `turn.completed`
// event:
//
//	{"type":"turn.completed","usage":{"input_tokens":14739,
//	 "cached_input_tokens":11520,"cache_write_input_tokens":0,
//	 "output_tokens":5,"reasoning_output_tokens":0}}
//
// Read off a real containerised run rather than inferred - this field was
// left unpopulated precisely because the schema had never been observed.
//
// There is NO cost figure, in any event. Under a ChatGPT subscription the
// vendor reports nothing billable, so CostUSD stays zero rather than being
// filled with an invented number: a budget expressed in dollars cannot be
// enforced here and should be expressed in runs. InputTokens counts the
// whole prompt including the cached portion, which is what was sent; the
// cached split is not carried because RunResult has nowhere honest to put
// it and a partial figure reads like a total.
//
// Later turns overwrite earlier ones: a run makes one turn, and if that
// ever changes the last completed turn is the one whose counts describe
// the result we keep.
func parseUsage(stdout []byte) Usage {
	var u Usage
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens           int `json:"input_tokens"`
				CachedInputTokens     int `json:"cached_input_tokens"`
				CacheWriteInputTokens int `json:"cache_write_input_tokens"`
				OutputTokens          int `json:"output_tokens"`
				ReasoningOutputTokens int `json:"reasoning_output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "turn.completed" || ev.Usage == nil {
			continue
		}
		u = Usage{
			InputTokens:  ev.Usage.InputTokens,
			CachedInput:  ev.Usage.CachedInputTokens,
			OutputTokens: ev.Usage.OutputTokens + ev.Usage.ReasoningOutputTokens,
			Found:        true,
		}
	}
	return u
}

// Usage is what the event stream actually reports, before it is narrowed to
// the adapter-neutral shape.
type Usage struct {
	InputTokens  int
	CachedInput  int
	OutputTokens int
	// Found distinguishes "the stream reported zero" from "no usage event
	// was seen at all", which is the difference between a free turn and a
	// parser that has stopped working.
	Found bool
}

// parseStreamError pulls the vendor's own message out of the event stream.
//
// codex reports failures as an `error` event on STDOUT, not on stderr:
//
//	{"type":"error","message":"You've hit your usage limit. ... try again at 2:30 PM."}
//
// stderr carries only "Reading prompt from stdin...", so an adapter error
// built from stderr alone says "exited with code 1" and nothing else. That
// is not merely unhelpful - the batch layer decides a stall from a rate
// limit by MATCHING THE ERROR TEXT, so a usage limit arrives as a plain
// failure, the case is recorded as failed instead of waited out, and the
// run burns through every remaining case producing failures. Observed
// exactly that: six consecutive "failures" that were one rate limit.
func parseStreamError(stdout []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var last string
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "error" || ev.Message == "" {
			continue
		}
		last = ev.Message
	}
	return last
}

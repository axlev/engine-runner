package codex

import "testing"

// The exact stream shape from a real containerised run. If codex changes
// it, this fails here rather than silently reporting zero tokens for every
// run - which is what the adapter did before, for exactly this reason.
const realEvents = `{"type":"thread.started","thread_id":"01a0"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"OK"}}
{"type":"turn.completed","usage":{"input_tokens":14739,"cached_input_tokens":11520,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}
`

func TestParseUsageReadsTheRealEventStream(t *testing.T) {
	u := parseUsage([]byte(realEvents))
	if !u.Found {
		t.Fatal("no usage event found in a stream that contains one")
	}
	if u.InputTokens != 14739 {
		t.Errorf("input tokens = %d, want 14739", u.InputTokens)
	}
	if u.CachedInput != 11520 {
		t.Errorf("cached input = %d, want 11520", u.CachedInput)
	}
	// Reasoning output is output the run paid for; folding it in stops a
	// reasoning-heavy turn reading as nearly free.
	if u.OutputTokens != 5 {
		t.Errorf("output tokens = %d, want 5", u.OutputTokens)
	}
}

func TestParseUsageFoldsReasoningIntoOutput(t *testing.T) {
	u := parseUsage([]byte(`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":3,"reasoning_output_tokens":40}}`))
	if u.OutputTokens != 43 {
		t.Errorf("output tokens = %d, want 43 (3 + 40 reasoning)", u.OutputTokens)
	}
}

// "No usage event" and "a turn that used nothing" are different failures
// and must not look alike: the first means the parser has stopped working.
func TestParseUsageDistinguishesAbsentFromZero(t *testing.T) {
	if u := parseUsage([]byte(`{"type":"turn.started"}`)); u.Found {
		t.Error("a stream with no usage event reported Found")
	}
	if u := parseUsage(nil); u.Found {
		t.Error("an empty stream reported Found")
	}
	u := parseUsage([]byte(`{"type":"turn.completed","usage":{"input_tokens":0,"output_tokens":0}}`))
	if !u.Found {
		t.Error("a genuinely zero turn must still be Found")
	}
}

func TestParseUsageIgnoresNoiseAndTakesTheLastTurn(t *testing.T) {
	stream := "Reading prompt from stdin...\nwarning: bubblewrap not on PATH\n" +
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":99,"output_tokens":7}}` + "\n"
	u := parseUsage([]byte(stream))
	if u.InputTokens != 99 || u.OutputTokens != 7 {
		t.Errorf("got %d/%d, want the last completed turn 99/7", u.InputTokens, u.OutputTokens)
	}
}

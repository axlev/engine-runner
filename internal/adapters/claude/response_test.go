package claude

import "testing"

// capturedUnauthenticatedResponse is the exact, unmodified stdout from a
// real `claude --bare -p "say hi" --output-format json` invocation with no
// credentials configured, captured in this session. It is real ground
// truth for the envelope shape, not a constructed guess - which matters
// because a documentation-based research pass into this same question
// separately claimed a much smaller, different envelope shape.
const capturedUnauthenticatedResponse = `{"duration_api_ms":0,"stop_reason":"stop_sequence","session_id":"6f47c9b7-69a3-4131-a069-a40e9b2455df","total_cost_usd":0,"usage":{"output_tokens_details":{"thinking_tokens":0},"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},"inference_geo":"","iterations":[],"speed":"standard"},"modelUsage":{},"permission_denials":[],"terminal_reason":"api_error","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subagent_stats":{"spawned":0,"requested":{"background":0,"foreground":0,"unset":0},"started_in_background":0,"max_depth":0,"spawned_by_subagents":0,"completed":0,"failed":0,"killed":{"parent":0,"user":0,"system":0},"refused":{"depth_limit":0,"concurrency_limit":0,"budget":0},"by_type":{}},"is_error":true,"num_turns":1,"subtype":"success","api_error_status":null,"result":"Not logged in · Please run /login","type":"result","duration_ms":97,"uuid":"470b0af9-0242-4b12-9398-35548529ff34","queued_turn_count":0}`

func TestParseResponseRealCapturedErrorEnvelope(t *testing.T) {
	resp, err := parseResponse([]byte(capturedUnauthenticatedResponse))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if !resp.IsError {
		t.Errorf("IsError = false, want true")
	}
	if resp.Result != "Not logged in · Please run /login" {
		t.Errorf("Result = %q, want the auth error message", resp.Result)
	}
	if resp.SessionID != "6f47c9b7-69a3-4131-a069-a40e9b2455df" {
		t.Errorf("SessionID = %q, want the captured session id", resp.SessionID)
	}
	if resp.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD = %v, want 0 for a failed call before any billing", resp.TotalCostUSD)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("Usage = %+v, want zero usage for a failed call", resp.Usage)
	}
}

// constructedSuccessResponse is NOT captured from a real run - no
// credentials were available in this environment to produce one. It is
// hand-built in the same envelope shape confirmed by the real error
// response above, with the fields a successful call would plausibly
// populate. Treat this test as verifying parseResponse's success path
// structurally, not as proof the real envelope looks exactly like this on
// success - that still needs checking against a real authenticated call.
const constructedSuccessResponse = `{"is_error":false,"result":"{\"schema_version\":\"review-a/v1\"}","total_cost_usd":0.0234,"session_id":"aaaa-bbbb","num_turns":1,"usage":{"input_tokens":1200,"output_tokens":340}}`

func TestParseResponseConstructedSuccessEnvelope(t *testing.T) {
	resp, err := parseResponse([]byte(constructedSuccessResponse))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.IsError {
		t.Errorf("IsError = true, want false")
	}
	if resp.Result != `{"schema_version":"review-a/v1"}` {
		t.Errorf("Result = %q, want the embedded review JSON text", resp.Result)
	}
	if resp.Usage.InputTokens != 1200 || resp.Usage.OutputTokens != 340 {
		t.Errorf("Usage = %+v, want {1200 340}", resp.Usage)
	}
	if resp.TotalCostUSD != 0.0234 {
		t.Errorf("TotalCostUSD = %v, want 0.0234", resp.TotalCostUSD)
	}
}

func TestParseResponseInvalidJSONIsError(t *testing.T) {
	if _, err := parseResponse([]byte("not json")); err == nil {
		t.Fatalf("expected an error for non-JSON stdout")
	}
}

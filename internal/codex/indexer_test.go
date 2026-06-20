package codex

import (
	"strings"
	"testing"
)

func TestParseFileUsesLastTokenUsage(t *testing.T) {
	input := strings.NewReader(`{"timestamp":"2026-06-20T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"model_context_window":258400}}}
{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":70,"output_tokens":20,"reasoning_output_tokens":3,"total_tokens":170},"last_token_usage":{"input_tokens":50,"cached_input_tokens":30,"output_tokens":10,"reasoning_output_tokens":1,"total_tokens":60},"model_context_window":258400}}}
`)

	events, _, err := ParseFile(input, "rollout.jsonl", "session", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].InputTokens != 50 || events[1].CachedInputTokens != 30 || events[1].TotalTokens != 60 {
		t.Fatalf("expected second event to use last_token_usage, got %+v", events[1])
	}
}

func TestParseFileFallsBackToCumulativeDelta(t *testing.T) {
	input := strings.NewReader(`{"timestamp":"2026-06-20T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110}}}}
{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":70,"output_tokens":20,"reasoning_output_tokens":3,"total_tokens":170}}}}
`)

	events, _, err := ParseFile(input, "rollout.jsonl", "session", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].InputTokens != 100 || events[0].TotalTokens != 110 {
		t.Fatalf("expected first cumulative event to be used as initial increment, got %+v", events[0])
	}
	if events[1].InputTokens != 50 || events[1].CachedInputTokens != 30 || events[1].TotalTokens != 60 {
		t.Fatalf("expected second event to use cumulative delta, got %+v", events[1])
	}
}

func TestParseFileSkipsMalformedAndInfoNullLines(t *testing.T) {
	input := strings.NewReader(`not-json
{"timestamp":"2026-06-20T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":null}}
{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}
`)

	events, _, err := ParseFile(input, "rollout.jsonl", "session", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].TotalTokens != 12 {
		t.Fatalf("expected total tokens 12, got %d", events[0].TotalTokens)
	}
}

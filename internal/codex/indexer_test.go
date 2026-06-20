package codex

import (
	"strings"
	"testing"
)

func TestParseFileUsesLastTokenUsage(t *testing.T) {
	input := strings.NewReader(`{"timestamp":"2026-06-20T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"model_context_window":258400}}}
{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":70,"output_tokens":20,"reasoning_output_tokens":3,"total_tokens":170},"last_token_usage":{"input_tokens":50,"cached_input_tokens":30,"output_tokens":10,"reasoning_output_tokens":1,"total_tokens":60},"model_context_window":258400}}}
`)

	result, err := ParseFile(input, "rollout.jsonl", "session", 0, rawUsage{}, "")
	if err != nil {
		t.Fatal(err)
	}
	events := result.Events
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

	result, err := ParseFile(input, "rollout.jsonl", "session", 0, rawUsage{}, "")
	if err != nil {
		t.Fatal(err)
	}
	events := result.Events
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

	result, err := ParseFile(input, "rollout.jsonl", "session", 0, rawUsage{}, "")
	if err != nil {
		t.Fatal(err)
	}
	events := result.Events
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].TotalTokens != 12 {
		t.Fatalf("expected total tokens 12, got %d", events[0].TotalTokens)
	}
}

func TestParseFileDedupesRepeatedCumulativeTotalsEvenWithLastUsage(t *testing.T) {
	input := strings.NewReader(`{"timestamp":"2026-06-20T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"model_context_window":258400}}}
{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"model_context_window":258400}}}
{"timestamp":"2026-06-20T10:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":70,"output_tokens":20,"reasoning_output_tokens":3,"total_tokens":180},"last_token_usage":{"input_tokens":60,"cached_input_tokens":30,"output_tokens":10,"reasoning_output_tokens":1,"total_tokens":70},"model_context_window":258400}}}
`)

	result, err := ParseFile(input, "rollout.jsonl", "session", 0, rawUsage{}, "")
	if err != nil {
		t.Fatal(err)
	}
	events := result.Events
	checkpoint := result.Checkpoint
	if len(events) != 2 {
		t.Fatalf("expected duplicate cumulative event to be skipped, got %d events", len(events))
	}
	if events[0].TotalTokens != 110 || events[1].TotalTokens != 70 {
		t.Fatalf("unexpected event totals after dedupe: %+v", events)
	}
	if checkpoint.TotalTokens != 180 {
		t.Fatalf("expected checkpoint to track final cumulative total 180, got %+v", checkpoint)
	}
}

func TestParseFileDedupesFirstAppendedEventAgainstSavedCheckpoint(t *testing.T) {
	input := strings.NewReader(`{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110},"model_context_window":258400}}}
{"timestamp":"2026-06-20T10:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":70,"output_tokens":20,"reasoning_output_tokens":3,"total_tokens":180},"last_token_usage":{"input_tokens":60,"cached_input_tokens":30,"output_tokens":10,"reasoning_output_tokens":1,"total_tokens":70},"model_context_window":258400}}}
`)
	checkpoint := rawUsage{
		InputTokens:           100,
		CachedInputTokens:     40,
		OutputTokens:          10,
		ReasoningOutputTokens: 2,
		TotalTokens:           110,
	}

	result, err := ParseFile(input, "rollout.jsonl", "session", 1024, checkpoint, "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	events := result.Events
	nextCheckpoint := result.Checkpoint
	if len(events) != 1 {
		t.Fatalf("expected first appended duplicate to be skipped, got %d events", len(events))
	}
	if events[0].TotalTokens != 70 {
		t.Fatalf("expected only the advanced event delta, got %+v", events[0])
	}
	if nextCheckpoint.TotalTokens != 180 {
		t.Fatalf("expected checkpoint to advance to 180, got %+v", nextCheckpoint)
	}
}

func TestParseFileExtractsSessionDirectoryAndFunctionCalls(t *testing.T) {
	input := strings.NewReader(`{"timestamp":"2026-06-20T09:00:00Z","type":"session_meta","payload":{"id":"session","cwd":"/Users/example/project"}}
{"timestamp":"2026-06-20T09:00:01Z","type":"turn_context","payload":{"model":"gpt-5.5","cwd":"/Users/example/project"}}
{"timestamp":"2026-06-20T09:01:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"rtk sed -n '1,20p' README.md\"}"}}
{"timestamp":"2026-06-20T09:02:00Z","type":"response_item","payload":{"type":"function_call","name":"apply_patch"}}
{"timestamp":"2026-06-20T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}
`)

	result, err := ParseFile(input, "rollout.jsonl", "session", 0, rawUsage{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionDir != "/Users/example/project" {
		t.Fatalf("expected session directory, got %q", result.SessionDir)
	}
	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 function calls, got %d", len(result.Commands))
	}
	if result.Commands[0].CommandName != "exec_command" || result.Commands[1].CommandName != "apply_patch" {
		t.Fatalf("unexpected command names: %+v", result.Commands)
	}
	if result.Model != "gpt-5.5" {
		t.Fatalf("expected model gpt-5.5, got %q", result.Model)
	}
	if len(result.Payloads) != 5 {
		t.Fatalf("expected 4 payload rows, got %d", len(result.Payloads))
	}
	if result.Payloads[2].NormalizedCommand != "sed" {
		t.Fatalf("expected rtk sed to normalize to sed, got %q", result.Payloads[2].NormalizedCommand)
	}
	if result.Payloads[4].PayloadType != "token_count" || result.Payloads[4].TotalTokens != 12 || result.Payloads[4].Model != "gpt-5.5" {
		t.Fatalf("expected token_count payload metadata, got %+v", result.Payloads[4])
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected token event to still be parsed, got %d", len(result.Events))
	}
	if result.Events[0].Model != "gpt-5.5" {
		t.Fatalf("expected token event model gpt-5.5, got %q", result.Events[0].Model)
	}
}

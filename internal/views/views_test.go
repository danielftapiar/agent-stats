package views

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieltapia/agent-stats/internal/store"
)

func TestLoadDailyAggregatesTokenEvents(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.SaveFileSync(ctx, store.SourceFile{
		Path:            "source.jsonl",
		SizeBytes:       10,
		ModTimeUnix:     1,
		ProcessedOffset: 10,
		SessionID:       "session-a",
	}, []store.TokenEvent{
		{
			SessionID:         "session-a",
			SourcePath:        "source.jsonl",
			Timestamp:         "2026-06-20T10:00:00Z",
			InputTokens:       10,
			CachedInputTokens: 20,
			OutputTokens:      5,
			TotalTokens:       15,
		},
		{
			SessionID:         "session-a",
			SourcePath:        "source.jsonl",
			Timestamp:         "2026-06-20T11:00:00Z",
			InputTokens:       20,
			CachedInputTokens: 40,
			OutputTokens:      10,
			TotalTokens:       30,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := Load(ctx, db, "daily", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 {
		t.Fatalf("expected 1 daily row, got %d", len(data.Rows))
	}
	if data.Rows[0].Label != "2026-06-20" {
		t.Fatalf("expected 2026-06-20 row, got %q", data.Rows[0].Label)
	}
	if data.Rows[0].Totals.TotalTokens != 45 {
		t.Fatalf("expected 45 total tokens, got %d", data.Rows[0].Totals.TotalTokens)
	}
	if data.Rows[0].Totals.CacheHitRate != float64(60)/float64(90) {
		t.Fatalf("unexpected cache hit rate %f", data.Rows[0].Totals.CacheHitRate)
	}
	if data.Rows[0].Totals.UncachedInputTokens != 30 {
		t.Fatalf("expected uncached input tokens 30, got %d", data.Rows[0].Totals.UncachedInputTokens)
	}
	if data.Rows[0].Totals.CacheReadInputTokens != 60 {
		t.Fatalf("expected cache read tokens 60, got %d", data.Rows[0].Totals.CacheReadInputTokens)
	}
}

func TestLoadSummaryDoesNotDoubleCountTokenTypeRows(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.SaveFileSync(ctx, store.SourceFile{
		Path:            "source.jsonl",
		SizeBytes:       10,
		ModTimeUnix:     1,
		ProcessedOffset: 10,
		SessionID:       "session-a",
	}, []store.TokenEvent{
		{
			SessionID:             "session-a",
			SourcePath:            "source.jsonl",
			Timestamp:             "2026-06-20T10:00:00Z",
			InputTokens:           10,
			CachedInputTokens:     20,
			OutputTokens:          5,
			ReasoningOutputTokens: 2,
			TotalTokens:           15,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := Load(ctx, db, "tokens", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if data.Totals.TotalTokens != 15 {
		t.Fatalf("expected database total_tokens 15, got %d", data.Totals.TotalTokens)
	}
	if data.Totals.CachedInputTokens != 20 {
		t.Fatalf("expected cached tokens to remain visible, got %d", data.Totals.CachedInputTokens)
	}
}

func TestLoadSummaryGroupsWeeklyCreditsAndFunctionCalls(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := store.SourceFile{
		Path:              "source.jsonl",
		SizeBytes:         10,
		ModTimeUnix:       1,
		ProcessedOffset:   10,
		SessionID:         "session-a",
		SessionDir:        "/Users/example/project",
		Model:             "gpt-5.5",
		FunctionCallCount: 1,
		LastSeenAt:        "2026-06-20T10:00:00Z",
	}
	events := []store.TokenEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:00:00Z", InputTokens: 1_000_000, CachedInputTokens: 1_000_000, OutputTokens: 500_000, ReasoningOutputTokens: 250_000, TotalTokens: 1_500_000, Model: "gpt-5.5"},
	}
	commands := []store.CommandEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:00:00Z", EventType: "function_call", CommandName: "exec_command", SessionDir: "/Users/example/project"},
	}
	if err := db.SaveFileSyncWithCommands(ctx, source, events, commands); err != nil {
		t.Fatal(err)
	}

	data, err := Load(ctx, db, "summary", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 {
		t.Fatalf("expected 1 weekly summary row, got %d", len(data.Rows))
	}
	if data.Rows[0].FunctionCalls != 1 {
		t.Fatalf("expected 1 function call, got %d", data.Rows[0].FunctionCalls)
	}
	if data.Rows[0].Totals.Credits != 4.25 {
		t.Fatalf("expected 4.25 credits, got %f", data.Rows[0].Totals.Credits)
	}
}

func TestLoadSessionsIncludesDirectoryAndFunctionCalls(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.SaveFileSync(ctx, store.SourceFile{
		Path:              "source.jsonl",
		SizeBytes:         10,
		ModTimeUnix:       1,
		ProcessedOffset:   10,
		SessionID:         "session-a",
		SessionDir:        "/Users/example/project",
		Model:             "gpt-5.5",
		FunctionCallCount: 7,
		LastSeenAt:        "2026-06-20T10:00:00Z",
	}, []store.TokenEvent{
		{
			SessionID:         "session-a",
			SourcePath:        "source.jsonl",
			Timestamp:         "2026-06-20T10:00:00Z",
			InputTokens:       1000,
			CachedInputTokens: 250,
			OutputTokens:      100,
			TotalTokens:       1100,
			Model:             "gpt-5.5",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := Load(ctx, db, "sessions", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 {
		t.Fatalf("expected 1 session row, got %d", len(data.Rows))
	}
	if data.Rows[0].Directory != "/Users/example/project" {
		t.Fatalf("expected session directory, got %q", data.Rows[0].Directory)
	}
	if data.Rows[0].Model != "gpt-5.5" {
		t.Fatalf("expected session model gpt-5.5, got %q", data.Rows[0].Model)
	}
	if data.Rows[0].FunctionCalls != 7 {
		t.Fatalf("expected 7 function calls, got %d", data.Rows[0].FunctionCalls)
	}
	if data.Rows[0].Totals.Credits == 0 {
		t.Fatal("expected session credits to be calculated")
	}

	rendered := Render(data, "sessions")
	if !strings.Contains(rendered, "Directory") || !strings.Contains(rendered, "Model") || !strings.Contains(rendered, "Credits") || !strings.Contains(rendered, "FCalls") {
		t.Fatalf("expected rendered sessions table to include Directory, Model, Credits and FCalls columns:\n%s", rendered)
	}
	if !strings.Contains(rendered, "example/project") || strings.Contains(rendered, "/Users/example/project") {
		t.Fatalf("expected rendered directory to use last two path components:\n%s", rendered)
	}
}

func TestRenderSessionsMarksSelectedRow(t *testing.T) {
	data := Data{
		View:          "sessions",
		SelectedIndex: 1,
		Rows: []Row{
			{Label: "session-a", Totals: withDerived(Totals{TotalTokens: 10, InputTokens: 10})},
			{Label: "session-b", Totals: withDerived(Totals{TotalTokens: 20, InputTokens: 20})},
		},
	}

	rendered := Render(data, "sessions")
	if !strings.Contains(rendered, "> session-b") {
		t.Fatalf("expected selected session marker on second row:\n%s", rendered)
	}
}

func TestLoadReasoningIncludesFunctionCallsByDay(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.SaveFileSync(ctx, store.SourceFile{
		Path:              "source.jsonl",
		SizeBytes:         10,
		ModTimeUnix:       1,
		ProcessedOffset:   10,
		SessionID:         "session-a",
		FunctionCallCount: 3,
		LastSeenAt:        "2026-06-20T10:00:00Z",
	}, []store.TokenEvent{
		{
			SessionID:             "session-a",
			SourcePath:            "source.jsonl",
			Timestamp:             "2026-06-20T10:00:00Z",
			InputTokens:           100,
			OutputTokens:          40,
			ReasoningOutputTokens: 25,
			TotalTokens:           140,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := Load(ctx, db, "reasoning", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 {
		t.Fatalf("expected 1 reasoning row, got %d", len(data.Rows))
	}
	if data.Rows[0].FunctionCalls != 3 {
		t.Fatalf("expected 3 function calls, got %d", data.Rows[0].FunctionCalls)
	}
	rendered := Render(data, "reasoning")
	if !strings.Contains(rendered, "FCalls") {
		t.Fatalf("expected rendered reasoning table to include FCalls column:\n%s", rendered)
	}
}

func TestLoadCommandsGroupsByCommandName(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := store.SourceFile{
		Path:              "source.jsonl",
		SizeBytes:         10,
		ModTimeUnix:       1,
		ProcessedOffset:   10,
		SessionID:         "session-a",
		SessionDir:        "/Users/example/project",
		FunctionCallCount: 3,
		LastSeenAt:        "2026-06-20T10:00:00Z",
	}
	commands := []store.CommandEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:00:00Z", EventType: "function_call", CommandName: "shell", SessionDir: "/Users/example/project"},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:01:00Z", EventType: "function_call", CommandName: "shell", SessionDir: "/Users/example/project"},
		{SessionID: "session-b", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:02:00Z", EventType: "function_call", CommandName: "apply_patch", SessionDir: "/Users/example/project"},
	}
	if err := db.SaveFileSyncWithCommands(ctx, source, nil, commands); err != nil {
		t.Fatal(err)
	}

	data, err := Load(ctx, db, "commands", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 command rows, got %d", len(data.Rows))
	}
	if data.Rows[0].Label != "apply_patch" {
		t.Fatalf("expected newest command to rank first, got %q", data.Rows[0].Label)
	}
	if data.Rows[0].EventType != "function_call" {
		t.Fatalf("expected function_call kind, got %q", data.Rows[0].EventType)
	}
	if data.Rows[0].FunctionCalls != 1 {
		t.Fatalf("expected 1 apply_patch call, got %d", data.Rows[0].FunctionCalls)
	}
	if data.Rows[0].SessionCount != 1 {
		t.Fatalf("expected 1 apply_patch session, got %d", data.Rows[0].SessionCount)
	}
	rendered := Render(data, "commands")
	for _, want := range []string{"Command", "Kind", "FCalls", "Sessions", "Directories"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered commands table to include %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "First Seen") || strings.Contains(rendered, "Last Seen") {
		t.Fatalf("expected rendered commands table to omit first/last seen columns:\n%s", rendered)
	}
}

func TestLoadPayloadSummariesAndSessionInteractions(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := store.SourceFile{
		Path:            "source.jsonl",
		SizeBytes:       10,
		ModTimeUnix:     1,
		ProcessedOffset: 10,
		SessionID:       "session-a",
		LastSeenAt:      "2026-06-20T10:02:00Z",
	}
	payloads := []store.PayloadEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:00:00Z", TopLevelType: "event_msg", PayloadType: "agent_message", Phase: "commentary", PayloadBytes: 100},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:01:00Z", TopLevelType: "response_item", PayloadType: "function_call", CommandName: "exec_command", NormalizedCommand: "sed", PayloadBytes: 200},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:01:30Z", TopLevelType: "response_item", PayloadType: "message", Role: "assistant", PayloadBytes: 150, InputTextCount: 1, InputTextBytes: 44},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:02:00Z", TopLevelType: "event_msg", PayloadType: "token_count", PayloadBytes: 300, InputTokens: 10, CachedInputTokens: 5, OutputTokens: 4, ReasoningOutputTokens: 1, TotalTokens: 14},
	}
	if err := db.SaveFileSyncWithDetails(ctx, source, nil, nil, payloads); err != nil {
		t.Fatal(err)
	}

	global, err := Load(ctx, db, "payload", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(global.Rows) != 4 {
		t.Fatalf("expected 4 payload groups, got %d", len(global.Rows))
	}
	if global.Rows[0].Count < global.Rows[len(global.Rows)-1].Count {
		t.Fatalf("expected payload groups ordered by count desc: %#v", global.Rows)
	}
	renderedGlobal := Render(global, "payload")
	if !strings.Contains(renderedGlobal, "Payload groups") || !strings.Contains(renderedGlobal, "Payload Bytes") {
		t.Fatalf("expected global payload summary table:\n%s", renderedGlobal)
	}
	if strings.Contains(renderedGlobal, "Last Seen") {
		t.Fatalf("expected global payload table to omit Last Seen:\n%s", renderedGlobal)
	}

	session, err := LoadSessionPayload(ctx, db, "session-a", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Rows) != 1 {
		t.Fatalf("expected 1 interaction row, got %d", len(session.Rows))
	}
	if session.Rows[0].Totals.TotalTokens != 14 {
		t.Fatalf("expected interaction total tokens 14, got %d", session.Rows[0].Totals.TotalTokens)
	}
	renderedSession := Render(session, "payload")
	for _, want := range []string{"Session: session-a", "top command", "input_text", "role count", "Interaction", "Payload Bytes"} {
		if !strings.Contains(renderedSession, want) {
			t.Fatalf("expected session payload drilldown to contain %q:\n%s", want, renderedSession)
		}
	}
	interaction, err := LoadPayloadInteraction(ctx, db, "session-a", "2026-06-20T10:02:00Z")
	if err != nil {
		t.Fatal(err)
	}
	renderedInteraction := Render(interaction, "payload")
	for _, want := range []string{"Interaction: 2026-06-20T10:02", "interaction payload", "event_msg count", "input_text", "top command", "role count"} {
		if !strings.Contains(renderedInteraction, want) {
			t.Fatalf("expected interaction drilldown to contain %q:\n%s", want, renderedInteraction)
		}
	}
}

func TestRenderSummaryAlignsValuesToColumns(t *testing.T) {
	data := Data{
		View: "summary",
		Rows: []Row{
			{Label: "2026-W24", FunctionCalls: 1234, LastSeen: "2026-06-20T10:00:00Z", Totals: withDerived(Totals{TotalTokens: 1740918485, InputTokens: 1733772383, CachedInputTokens: 1656928512, OutputTokens: 5163766, ReasoningOutputTokens: 1770977, Credits: 4200.5})},
			{Label: "2026-W23", FunctionCalls: 12, LastSeen: "2026-06-13T10:00:00Z", Totals: withDerived(Totals{TotalTokens: 1200, InputTokens: 1000, CachedInputTokens: 200, OutputTokens: 200, Credits: 0.5})},
		},
	}

	lines := strings.Split(Render(data, "summary"), "\n")
	headerIndex := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "Week ") {
			headerIndex = i
			break
		}
	}
	if headerIndex == -1 {
		t.Fatal("summary table header not found")
	}

	expectedHeader := []string{"Week", "Credits", "Total", "Uncached", "Cache Read", "Cache Hit", "FCalls"}
	expectedRows := [][]string{
		{"2026-W24", "4.2K", "1.74B", "1.73B", "1.66B", "48.9%", "1.23K"},
		{"2026-W23", "0.5", "1.2K", "1K", "200", "16.7%", "12"},
	}
	columns := columnsFor(append([][]string{expectedHeader}, expectedRows...))
	assertTableLineAligned(t, lines[headerIndex], columns, expectedHeader)
	for i, expected := range expectedRows {
		lineIndex := headerIndex + 1 + i
		if lineIndex >= len(lines) {
			t.Fatalf("missing table row %d", i)
		}
		assertTableLineAligned(t, lines[lineIndex], columns, expected)
	}
}

func TestCacheHitRateUsesArticleFormula(t *testing.T) {
	totals := Totals{
		UncachedInputTokens:      25,
		CacheReadInputTokens:     70,
		CacheCreationInputTokens: 5,
	}

	got := cacheHitRate(totals)
	want := 0.7
	if got != want {
		t.Fatalf("expected hit rate cache_read/(cache_read+cache_creation+uncached_input) = %f, got %f", want, got)
	}
}

func TestFormatIntUsesCompactSuffixes(t *testing.T) {
	tests := map[int64]string{
		0:                 "0",
		999:               "999",
		1000:              "1K",
		1532:              "1.53K",
		12_500:            "12.5K",
		125_000:           "125K",
		1_731_732_016:     "1.73B",
		2_500_000_000_000: "2.5T",
	}
	for input, want := range tests {
		if got := formatInt(input); got != want {
			t.Fatalf("formatInt(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestRenderGraphUsesCompactYAxisUnits(t *testing.T) {
	data := Data{
		View: "daily",
		Totals: Totals{
			TotalTokens: 1_500_000_000,
		},
		Rows: []Row{
			{Label: "2026-06-20", Totals: withDerived(Totals{TotalTokens: 1_500_000_000, InputTokens: 1_500_000_000})},
			{Label: "2026-06-21", Totals: withDerived(Totals{TotalTokens: 750_000_000, InputTokens: 750_000_000})},
		},
	}

	rendered := Render(data, "daily")
	if !strings.Contains(rendered, "B") && !strings.Contains(rendered, "M") {
		t.Fatalf("expected compact graph y-axis labels, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "1500000000") {
		t.Fatalf("expected graph y-axis to avoid raw long numbers, got:\n%s", rendered)
	}
}

func assertTableLineAligned(t *testing.T, line string, columns []tableColumn, expected []string) {
	t.Helper()
	start := 0
	for i, col := range columns {
		if i > 0 {
			start++
		}
		end := start + col.width
		if len(line) < end {
			t.Fatalf("line %q shorter than column %d ending at %d", line, i, end)
		}
		cell := line[start:end]
		value := strings.TrimSpace(cell)
		if value != expected[i] {
			t.Fatalf("column %d expected value %q, got %q in line %q", i, expected[i], value, line)
		}
		leftPadding := len(cell) - len(strings.TrimLeft(cell, " "))
		rightPadding := len(cell) - len(strings.TrimRight(cell, " "))
		switch col.align {
		case alignLeft:
			if leftPadding != 0 {
				t.Fatalf("column %d expected left alignment, got %d leading spaces in %q", i, leftPadding, cell)
			}
		case alignCenter:
			if diff := leftPadding - rightPadding; diff < -1 || diff > 1 {
				t.Fatalf("column %d expected centered value, got left=%d right=%d in %q", i, leftPadding, rightPadding, cell)
			}
		}
		start = end
	}
}

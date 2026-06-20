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

	data, err := Load(ctx, db, "summary", 20, time.Now())
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
	if data.Rows[0].FunctionCalls != 7 {
		t.Fatalf("expected 7 function calls, got %d", data.Rows[0].FunctionCalls)
	}

	rendered := Render(data, "sessions")
	if !strings.Contains(rendered, "Directory") || !strings.Contains(rendered, "Calls") {
		t.Fatalf("expected rendered sessions table to include Directory and Calls columns:\n%s", rendered)
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
	if !strings.Contains(rendered, "Calls") {
		t.Fatalf("expected rendered reasoning table to include Calls column:\n%s", rendered)
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
	if data.Rows[0].Label != "shell" {
		t.Fatalf("expected shell to rank first, got %q", data.Rows[0].Label)
	}
	if data.Rows[0].EventType != "function_call" {
		t.Fatalf("expected function_call kind, got %q", data.Rows[0].EventType)
	}
	if data.Rows[0].FunctionCalls != 2 {
		t.Fatalf("expected 2 shell calls, got %d", data.Rows[0].FunctionCalls)
	}
	if data.Rows[0].SessionCount != 1 {
		t.Fatalf("expected 1 shell session, got %d", data.Rows[0].SessionCount)
	}
	rendered := Render(data, "commands")
	for _, want := range []string{"Command", "Kind", "Calls", "Sessions", "Directories", "First Seen", "Last Seen"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered commands table to include %q:\n%s", want, rendered)
		}
	}
}

func TestRenderSummaryAlignsValuesToColumns(t *testing.T) {
	data := Data{
		View: "summary",
		Totals: Totals{
			InputTokens:           1733772383,
			CachedInputTokens:     1656928512,
			OutputTokens:          5163766,
			ReasoningOutputTokens: 1770977,
			TotalTokens:           1740918485,
			CacheHitRate:          0.9787,
		},
		Rows: []Row{
			{Label: "uncached input", Totals: withDerived(Totals{TotalTokens: 1733772383, InputTokens: 1733772383})},
			{Label: "cache read", Totals: withDerived(Totals{TotalTokens: 1656928512, CachedInputTokens: 1656928512})},
			{Label: "cache creation", Totals: withDerived(Totals{})},
			{Label: "output", Totals: Totals{TotalTokens: 5163766, OutputTokens: 5163766}},
			{Label: "reasoning output", Totals: Totals{TotalTokens: 1770977, ReasoningOutputTokens: 1770977}},
		},
	}

	lines := strings.Split(Render(data, "summary"), "\n")
	headerIndex := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "Group") {
			headerIndex = i
			break
		}
	}
	if headerIndex == -1 {
		t.Fatal("summary table header not found")
	}

	expectedHeader := []string{"Group", "Total", "Uncached", "Cache Read", "Output", "Reasoning", "Hit Rate"}
	expectedRows := [][]string{
		{"uncached input", "1.73B", "1.73B", "0", "0", "0", "0.0%"},
		{"cache read", "1.66B", "0", "1.66B", "0", "0", "100.0%"},
		{"cache creation", "0", "0", "0", "0", "0", "0.0%"},
		{"output", "5.16M", "0", "0", "5.16M", "0", "0.0%"},
		{"reasoning output", "1.77M", "0", "0", "0", "1.77M", "0.0%"},
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

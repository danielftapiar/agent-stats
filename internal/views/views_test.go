package views

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieltapia/agent-stats/internal/store"
)

func TestLoadTodayAggregatesTokenEvents(t *testing.T) {
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

	data, err := Load(ctx, db, "today", 20, time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 {
		t.Fatalf("expected 1 today row, got %d", len(data.Rows))
	}
	if data.Rows[0].Label != "session-a" {
		t.Fatalf("expected session-a row, got %q", data.Rows[0].Label)
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
	if len(data.GraphRows) != 24 {
		t.Fatalf("expected 24 hourly graph rows, got %d", len(data.GraphRows))
	}
	if data.GraphRows[0].Label != "00:00" || data.GraphRows[23].Label != "23:59" {
		t.Fatalf("expected graph to span 00:00 through 23:00, got first=%q last=%q", data.GraphRows[0].Label, data.GraphRows[23].Label)
	}
	if data.GraphRows[10].Totals.TotalTokens != 15 || data.GraphRows[11].Totals.TotalTokens != 30 {
		t.Fatalf("expected hourly token totals at 10:00 and 11:00, got %d and %d", data.GraphRows[10].Totals.TotalTokens, data.GraphRows[11].Totals.TotalTokens)
	}
}

func TestWithDerivedTreatsCachedInputAsInputSubset(t *testing.T) {
	totals := withDerived(Totals{
		InputTokens:       1_000,
		CachedInputTokens: 750,
	})

	if totals.UncachedInputTokens != 250 {
		t.Fatalf("expected uncached input to be input-cached, got %d", totals.UncachedInputTokens)
	}
	if totals.CacheHitRate != 0.75 {
		t.Fatalf("expected cache hit rate 0.75, got %f", totals.CacheHitRate)
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
		LastSeenAt:        "2026-05-18T10:00:00Z",
	}
	events := []store.TokenEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-18T10:00:00Z", InputTokens: 1_000_000, CachedInputTokens: 1_000_000, OutputTokens: 500_000, ReasoningOutputTokens: 250_000, TotalTokens: 1_500_000, Model: "gpt-5.5"},
	}
	commands := []store.CommandEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-18T10:00:00Z", EventType: "function_call", CommandName: "exec_command", SessionDir: "/Users/example/project"},
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
	if data.Rows[0].Label != "2026 May 18th" {
		t.Fatalf("expected Monday week label 2026 May 18th, got %q", data.Rows[0].Label)
	}
	if data.Rows[0].FunctionCalls != 1 {
		t.Fatalf("expected 1 function call, got %d", data.Rows[0].FunctionCalls)
	}
	if data.Rows[0].Totals.Credits != 80.5 {
		t.Fatalf("expected 80.5 credits, got %f", data.Rows[0].Totals.Credits)
	}
}

func TestLoadSummaryWeekGroupsDaysWithGraph(t *testing.T) {
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
		Model:           "gpt-5.5",
	}
	events := []store.TokenEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-18T10:00:00Z", InputTokens: 1_000_000, CachedInputTokens: 900_000, OutputTokens: 10_000, TotalTokens: 1_010_000, Model: "gpt-5.5"},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-19T10:00:00Z", InputTokens: 2_000_000, CachedInputTokens: 1_500_000, OutputTokens: 20_000, TotalTokens: 2_020_000, Model: "gpt-5.5"},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-25T10:00:00Z", InputTokens: 9_000_000, CachedInputTokens: 8_000_000, OutputTokens: 90_000, TotalTokens: 9_090_000, Model: "gpt-5.5"},
	}
	commands := []store.CommandEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-18T10:00:00Z", EventType: "function_call", CommandName: "exec_command"},
	}
	if err := db.SaveFileSyncWithDetails(ctx, source, events, commands, nil); err != nil {
		t.Fatal(err)
	}

	data, err := LoadSummaryWeek(ctx, db, "2026-05-18")
	if err != nil {
		t.Fatal(err)
	}
	if data.Period != "day" || data.PeriodStart != "2026-05-18" {
		t.Fatalf("expected day summary for 2026-05-18, got period=%q start=%q", data.Period, data.PeriodStart)
	}
	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 day rows in selected week, got %d", len(data.Rows))
	}
	rendered := Render(data, "summary")
	for _, want := range []string{"Week 2026 May 18th daily credits", "Day", "2026 May 19th", "2026 May 18th"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected weekly drilldown summary to contain %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Days:") {
		t.Fatalf("expected summary graph x-axis labels to be omitted:\n%s", rendered)
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
	if !strings.Contains(rendered, selectedRowMarker+"session-b") {
		t.Fatalf("expected internal selected session marker on second row:\n%s", rendered)
	}
	if strings.Contains(rendered, "> session-b") {
		t.Fatalf("expected selected row not to render visible > marker:\n%s", rendered)
	}
}

func TestRenderRowsPlacesCreditsImmediatelyBeforeFCalls(t *testing.T) {
	data := Data{
		View: "sessions",
		Rows: []Row{{
			Label:         "session-a",
			FunctionCalls: 3,
			Totals:        withDerived(Totals{TotalTokens: 10, InputTokens: 10, Credits: 42}),
		}},
	}

	rendered := Render(data, "sessions")
	var header string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "Group") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatalf("expected sessions table header:\n%s", rendered)
	}
	headerIndex := strings.Index(header, "Credits")
	if headerIndex == -1 {
		t.Fatalf("expected Credits column:\n%s", header)
	}
	fcallsIndex := strings.Index(header, "FCalls")
	if fcallsIndex == -1 {
		t.Fatalf("expected FCalls column:\n%s", header)
	}
	if headerIndex > fcallsIndex {
		t.Fatalf("expected Credits to be left of FCalls:\n%s", header)
	}
	between := header[headerIndex+len("Credits") : fcallsIndex]
	if strings.Contains(strings.TrimSpace(between), "Total") {
		t.Fatalf("expected Credits immediately before FCalls, got intervening header content %q:\n%s", between, header)
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

func TestLoadPayloadSummariesAndSessionResponses(t *testing.T) {
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
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:01:00Z", TopLevelType: "response_item", PayloadType: "function_call", CommandName: "exec_command", NormalizedCommand: "sed", Arguments: `{"cmd":"rtk sed -n '1,20p' README.md"}`, ArgumentsBytes: 41, PayloadBytes: 2048, DurationMS: 20},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:01:20Z", TopLevelType: "response_item", PayloadType: "function_call_output", ResponseOutputBytes: 4096, PayloadBytes: 4096, DurationMS: 30},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:01:30Z", TopLevelType: "response_item", PayloadType: "message", Role: "assistant", PayloadBytes: 3072, InputTextCount: 1, InputTextBytes: 44, ResponseOutputBytes: 2048},
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T10:02:00Z", TopLevelType: "event_msg", PayloadType: "token_count", PayloadBytes: 300, InputTokens: 10, CachedInputTokens: 5, OutputTokens: 4, ReasoningOutputTokens: 1, TotalTokens: 14},
	}
	if err := db.SaveFileSyncWithDetails(ctx, source, nil, nil, payloads); err != nil {
		t.Fatal(err)
	}

	global, err := Load(ctx, db, "payload", 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(global.Rows) != 5 {
		t.Fatalf("expected 5 payload groups, got %d", len(global.Rows))
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
	if len(session.Rows) != 3 {
		t.Fatalf("expected 3 response aggregate rows, got %d", len(session.Rows))
	}
	if session.Rows[0].ResponseOutputBytes != 2048 {
		t.Fatalf("expected latest message response output bytes 2048, got %d", session.Rows[0].ResponseOutputBytes)
	}
	renderedSession := Render(session, "payload")
	for _, want := range []string{"Session: session-a", "session_duration", "Avg Duration", "command", "input_text", "role count", "Function calls", "Type", "Arguments", "Output Bytes", "function_call", "sed", "2kb", "4kb"} {
		if !strings.Contains(renderedSession, want) {
			t.Fatalf("expected session payload drilldown to contain %q:\n%s", want, renderedSession)
		}
	}
	if strings.Contains(renderedSession, "Interaction") {
		t.Fatalf("expected session payload drilldown to omit interaction list:\n%s", renderedSession)
	}
	if strings.Contains(renderedSession, "prompt to final answer") {
		t.Fatalf("expected session payload metadata to omit prompt-final metric:\n%s", renderedSession)
	}
	topMetadata := strings.Split(renderedSession, "Function calls")[0]
	if strings.Contains(topMetadata, "Max Dur") || strings.Contains(topMetadata, "Avg TTFT") {
		t.Fatalf("expected right-side session metadata to omit timing columns:\n%s", renderedSession)
	}
	if !strings.Contains(renderedSession, "Function calls") || !strings.Contains(renderedSession, "Avg Dur") {
		t.Fatalf("expected function calls section to keep timing columns:\n%s", renderedSession)
	}
}

func TestRenderSummaryAlignsValuesToColumns(t *testing.T) {
	data := Data{
		View: "summary",
		Rows: []Row{
			{Label: "2026 May 18th", FunctionCalls: 1234, LastSeen: "2026-06-20T10:00:00Z", Totals: withDerived(Totals{TotalTokens: 1740918485, InputTokens: 1733772383, CachedInputTokens: 1656928512, OutputTokens: 5163766, ReasoningOutputTokens: 1770977, Credits: 4200.5})},
			{Label: "2026 May 11th", FunctionCalls: 12, LastSeen: "2026-06-13T10:00:00Z", Totals: withDerived(Totals{TotalTokens: 1200, InputTokens: 1000, CachedInputTokens: 200, OutputTokens: 200, Credits: 0.5})},
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

	rendered := strings.Join(lines, "\n")
	for _, want := range []string{
		"Week", "Budget", "Text Tokens", "Uncached", "Cache Read", "Cache Hit", "FCalls",
		"2026 May 18th", "████████░░░░░░░░░░░░ 4.2K/10K", "1.74B", "76.8M", "1.66B", "95.6%", "1.23K",
		"2026 May 11th", "░░░░░░░░░░░░░░░░░░░░ 0.5/10K", "1.2K",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered summary to contain %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Week             Credits") {
		t.Fatalf("expected summary table to omit standalone Credits column:\n%s", rendered)
	}
	if strings.Contains(rendered, "Weeks:") {
		t.Fatalf("expected summary graph x-axis labels to be omitted:\n%s", rendered)
	}
	if strings.Contains(rendered, "#") {
		t.Fatalf("expected summary progress bars to use rendered block glyphs, got:\n%s", rendered)
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
		View: "today",
		Totals: Totals{
			TotalTokens: 1_500_000_000,
		},
		Rows: []Row{
			{Label: "2026-06-20", Totals: withDerived(Totals{TotalTokens: 1_500_000_000, InputTokens: 1_500_000_000})},
			{Label: "2026-06-21", Totals: withDerived(Totals{TotalTokens: 750_000_000, InputTokens: 750_000_000})},
		},
	}

	rendered := Render(data, "today")
	if !strings.Contains(rendered, "B") && !strings.Contains(rendered, "M") {
		t.Fatalf("expected compact graph y-axis labels, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "1500000000") {
		t.Fatalf("expected graph y-axis to avoid raw long numbers, got:\n%s", rendered)
	}
}

func TestRenderWithWidthStretchesSummaryGraphAndTable(t *testing.T) {
	data := Data{
		View: "summary",
		Rows: []Row{
			{Label: "2026 May 18th", Totals: withDerived(Totals{InputTokens: 1000, TotalTokens: 1000, Credits: 20})},
			{Label: "2026 May 11th", Totals: withDerived(Totals{InputTokens: 500, TotalTokens: 500, Credits: 10})},
		},
	}

	rendered := RenderWithWidth(data, "summary", 140)
	if maxGraphWidth(rendered) < 140 {
		t.Fatalf("expected summary graph to stretch to at least 140 cells:\n%s", rendered)
	}
	if tableHeaderWidth(rendered, "Week") < 140 {
		t.Fatalf("expected summary table header to stretch to 140 cells:\n%s", rendered)
	}
}

func TestRenderWithWidthStretchesSessionTable(t *testing.T) {
	data := Data{
		View: "sessions",
		Rows: []Row{{
			Label:     "session-a",
			Directory: "/Users/example/project",
			Model:     "gpt-5.5",
			Totals:    withDerived(Totals{InputTokens: 1000, TotalTokens: 1000, Credits: 20}),
		}},
	}

	rendered := RenderWithWidth(data, "sessions", 160)
	if tableHeaderWidth(rendered, "Group") < 160 {
		t.Fatalf("expected sessions table header to stretch to 160 cells:\n%s", rendered)
	}
}

func maxGraphWidth(rendered string) int {
	maxWidth := 0
	for _, line := range strings.Split(rendered, "\n") {
		if strings.ContainsAny(line, "┤┼╭╮╯╰─│") {
			if width := displayWidth(line); width > maxWidth {
				maxWidth = width
			}
		}
	}
	return maxWidth
}

func tableHeaderWidth(rendered, prefix string) int {
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, prefix) && isExpectedTableHeader(line, prefix) {
			return displayWidth(line)
		}
	}
	return 0
}

func isExpectedTableHeader(line, prefix string) bool {
	switch prefix {
	case "Week":
		return strings.Contains(line, "Budget")
	case "Group":
		return strings.Contains(line, "Credits")
	default:
		return true
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

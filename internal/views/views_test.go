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
			{Label: "input", Totals: Totals{TotalTokens: 1733772383, InputTokens: 1733772383}},
			{Label: "cached input", Totals: Totals{TotalTokens: 1656928512, CachedInputTokens: 1656928512}},
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

	expectedHeader := []string{"Group", "Total", "Input", "Cached", "Output", "Reasoning", "Cache"}
	expectedRows := [][]string{
		{"input", "1.73B", "1.73B", "0", "0", "0", "0.0%"},
		{"cached input", "1.66B", "0", "1.66B", "0", "0", "0.0%"},
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

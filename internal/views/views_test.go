package views

import (
	"context"
	"path/filepath"
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

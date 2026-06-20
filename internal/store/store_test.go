package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDeleteSessionRemovesIndexedRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := SourceFile{
		Path:            "session-a.jsonl",
		SizeBytes:       10,
		ModTimeUnix:     1,
		ProcessedOffset: 10,
		SessionID:       "session-a",
	}
	events := []TokenEvent{{SessionID: "session-a", SourcePath: source.Path, Timestamp: "2026-06-20T10:00:00Z", InputTokens: 1, TotalTokens: 1}}
	commands := []CommandEvent{{SessionID: "session-a", SourcePath: source.Path, Timestamp: "2026-06-20T10:00:01Z", EventType: "function_call", CommandName: "exec_command"}}
	payloads := []PayloadEvent{{SessionID: "session-a", SourcePath: source.Path, Timestamp: "2026-06-20T10:00:02Z", TopLevelType: "event_msg", PayloadType: "token_count", PayloadBytes: 10}}
	if err := db.SaveFileSyncWithDetails(ctx, source, events, commands, payloads); err != nil {
		t.Fatal(err)
	}

	paths, err := db.SessionSourcePaths(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != source.Path {
		t.Fatalf("expected source path before deletion, got %#v", paths)
	}
	if err := db.DeleteSession(ctx, "session-a"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM source_files WHERE session_id = 'session-a'`,
		`SELECT COUNT(*) FROM token_events WHERE session_id = 'session-a'`,
		`SELECT COUNT(*) FROM command_events WHERE session_id = 'session-a'`,
		`SELECT COUNT(*) FROM payload_events WHERE session_id = 'session-a'`,
	} {
		rows, err := db.Query(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if rows.Next() {
			if err := rows.Scan(&count); err != nil {
				rows.Close()
				t.Fatal(err)
			}
		}
		rows.Close()
		if count != 0 {
			t.Fatalf("expected query %q to return 0 rows, got %d", query, count)
		}
	}
}

package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

type SourceFile struct {
	Path            string
	SizeBytes       int64
	ModTimeUnix     int64
	ProcessedOffset int64
	SessionID       string
	StartedAt       string
	LastSeenAt      string
}

type TokenEvent struct {
	ID                    int64  `json:"id,omitempty"`
	SessionID             string `json:"session_id"`
	SourcePath            string `json:"source_path"`
	Timestamp             string `json:"timestamp"`
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	ModelContextWindow    int64  `json:"model_context_window,omitempty"`
}

func DefaultCachePath() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "agent-stats", "codex-usage.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "codex-usage.db"
	}
	return filepath.Join(home, ".cache", "agent-stats", "codex-usage.db")
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	wrapped := &DB{sql: db}
	if err := wrapped.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return wrapped, nil
}

func (db *DB) Close() error {
	return db.sql.Close()
}

func (db *DB) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS source_files (
			path TEXT PRIMARY KEY,
			size_bytes INTEGER NOT NULL,
			mod_time_unix INTEGER NOT NULL,
			processed_offset INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			started_at TEXT,
			last_seen_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS token_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			source_path TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			input_tokens INTEGER NOT NULL,
			cached_input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			reasoning_output_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			model_context_window INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS token_events_timestamp_idx ON token_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS token_events_session_idx ON token_events(session_id)`,
		`CREATE INDEX IF NOT EXISTS token_events_source_path_idx ON token_events(source_path)`,
	}
	for _, stmt := range statements {
		if _, err := db.sql.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) SourceFile(ctx context.Context, path string) (SourceFile, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT path, size_bytes, mod_time_unix, processed_offset, session_id, COALESCE(started_at, ''), COALESCE(last_seen_at, '') FROM source_files WHERE path = ?`, path)
	var sf SourceFile
	if err := row.Scan(&sf.Path, &sf.SizeBytes, &sf.ModTimeUnix, &sf.ProcessedOffset, &sf.SessionID, &sf.StartedAt, &sf.LastSeenAt); err != nil {
		if err == sql.ErrNoRows {
			return SourceFile{}, false, nil
		}
		return SourceFile{}, false, err
	}
	return sf, true, nil
}

func (db *DB) DeleteSourceFileEvents(ctx context.Context, path string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_events WHERE source_path = ?`, path); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_files WHERE path = ?`, path); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) SaveFileSync(ctx context.Context, source SourceFile, events []TokenEvent) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO token_events (
			session_id, source_path, timestamp, input_tokens, cached_input_tokens, output_tokens,
			reasoning_output_tokens, total_tokens, model_context_window
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.SessionID,
			event.SourcePath,
			event.Timestamp,
			event.InputTokens,
			event.CachedInputTokens,
			event.OutputTokens,
			event.ReasoningOutputTokens,
			event.TotalTokens,
			event.ModelContextWindow,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_files (
		path, size_bytes, mod_time_unix, processed_offset, session_id, started_at, last_seen_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(path) DO UPDATE SET
		size_bytes = excluded.size_bytes,
		mod_time_unix = excluded.mod_time_unix,
		processed_offset = excluded.processed_offset,
		session_id = excluded.session_id,
		started_at = COALESCE(NULLIF(source_files.started_at, ''), excluded.started_at),
		last_seen_at = excluded.last_seen_at`,
		source.Path,
		source.SizeBytes,
		source.ModTimeUnix,
		source.ProcessedOffset,
		source.SessionID,
		source.StartedAt,
		source.LastSeenAt,
	); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.sql.QueryContext(ctx, query, args...)
}

func (db *DB) Events(ctx context.Context) ([]TokenEvent, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id, session_id, source_path, timestamp, input_tokens, cached_input_tokens, output_tokens, reasoning_output_tokens, total_tokens, COALESCE(model_context_window, 0) FROM token_events ORDER BY timestamp, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []TokenEvent
	for rows.Next() {
		var event TokenEvent
		if err := rows.Scan(
			&event.ID,
			&event.SessionID,
			&event.SourcePath,
			&event.Timestamp,
			&event.InputTokens,
			&event.CachedInputTokens,
			&event.OutputTokens,
			&event.ReasoningOutputTokens,
			&event.TotalTokens,
			&event.ModelContextWindow,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

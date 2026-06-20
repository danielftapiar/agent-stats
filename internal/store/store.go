package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

type SourceFile struct {
	Path                       string
	SizeBytes                  int64
	ModTimeUnix                int64
	ProcessedOffset            int64
	SessionID                  string
	SessionDir                 string
	Model                      string
	FunctionCallCount          int64
	PayloadMetricsVersion      int64
	StartedAt                  string
	LastSeenAt                 string
	LastTotalInputTokens       int64
	LastTotalCachedInputTokens int64
	LastTotalOutputTokens      int64
	LastTotalReasoningTokens   int64
	LastTotalTokens            int64
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
	Model                 string `json:"model,omitempty"`
}

type CommandEvent struct {
	ID          int64  `json:"id,omitempty"`
	SessionID   string `json:"session_id"`
	SourcePath  string `json:"source_path"`
	Timestamp   string `json:"timestamp"`
	EventType   string `json:"event_type"`
	CommandName string `json:"command_name"`
	SessionDir  string `json:"session_dir,omitempty"`
}

type PayloadEvent struct {
	ID                    int64  `json:"id,omitempty"`
	SessionID             string `json:"session_id"`
	SourcePath            string `json:"source_path"`
	Timestamp             string `json:"timestamp"`
	TopLevelType          string `json:"top_level_type"`
	PayloadType           string `json:"payload_type"`
	Phase                 string `json:"phase,omitempty"`
	PayloadBytes          int64  `json:"payload_bytes"`
	ContentBytes          int64  `json:"content_bytes,omitempty"`
	Role                  string `json:"role,omitempty"`
	InputTextCount        int64  `json:"input_text_count,omitempty"`
	InputTextBytes        int64  `json:"input_text_bytes,omitempty"`
	CompletedAt           string `json:"completed_at,omitempty"`
	DurationMS            int64  `json:"duration_ms,omitempty"`
	TimeToFirstTokenMS    int64  `json:"time_to_first_token_ms,omitempty"`
	CommandName           string `json:"command_name,omitempty"`
	NormalizedCommand     string `json:"normalized_command,omitempty"`
	CallID                string `json:"call_id,omitempty"`
	InputTokens           int64  `json:"input_tokens,omitempty"`
	CachedInputTokens     int64  `json:"cached_input_tokens,omitempty"`
	OutputTokens          int64  `json:"output_tokens,omitempty"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens,omitempty"`
	TotalTokens           int64  `json:"total_tokens,omitempty"`
	ModelContextWindow    int64  `json:"model_context_window,omitempty"`
	Model                 string `json:"model,omitempty"`
	PayloadJSON           string `json:"payload_json,omitempty"`
	RawJSON               string `json:"raw_json,omitempty"`
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
			session_dir TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			function_call_count INTEGER NOT NULL DEFAULT 0,
			payload_metrics_version INTEGER NOT NULL DEFAULT 0,
			started_at TEXT,
			last_seen_at TEXT,
			last_total_input_tokens INTEGER NOT NULL DEFAULT 0,
			last_total_cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			last_total_output_tokens INTEGER NOT NULL DEFAULT 0,
			last_total_reasoning_tokens INTEGER NOT NULL DEFAULT 0,
			last_total_tokens INTEGER NOT NULL DEFAULT 0
		)`,
		`ALTER TABLE source_files ADD COLUMN last_total_input_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_files ADD COLUMN last_total_cached_input_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_files ADD COLUMN last_total_output_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_files ADD COLUMN last_total_reasoning_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_files ADD COLUMN last_total_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_files ADD COLUMN session_dir TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE source_files ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE source_files ADD COLUMN function_call_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_files ADD COLUMN payload_metrics_version INTEGER NOT NULL DEFAULT 0`,
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
			model_context_window INTEGER,
			model TEXT NOT NULL DEFAULT ''
		)`,
		`ALTER TABLE token_events ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS token_events_timestamp_idx ON token_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS token_events_session_idx ON token_events(session_id)`,
		`CREATE INDEX IF NOT EXISTS token_events_source_path_idx ON token_events(source_path)`,
		`CREATE TABLE IF NOT EXISTS command_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			source_path TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			event_type TEXT NOT NULL,
			command_name TEXT NOT NULL,
			session_dir TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS command_events_timestamp_idx ON command_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS command_events_session_idx ON command_events(session_id)`,
		`CREATE INDEX IF NOT EXISTS command_events_source_path_idx ON command_events(source_path)`,
		`CREATE INDEX IF NOT EXISTS command_events_command_name_idx ON command_events(command_name)`,
		`CREATE TABLE IF NOT EXISTS payload_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			source_path TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			top_level_type TEXT NOT NULL,
			payload_type TEXT NOT NULL,
			phase TEXT NOT NULL DEFAULT '',
			payload_bytes INTEGER NOT NULL,
			content_bytes INTEGER NOT NULL DEFAULT 0,
			role TEXT NOT NULL DEFAULT '',
			input_text_count INTEGER NOT NULL DEFAULT 0,
			input_text_bytes INTEGER NOT NULL DEFAULT 0,
			completed_at TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			time_to_first_token_ms INTEGER NOT NULL DEFAULT 0,
			command_name TEXT NOT NULL DEFAULT '',
			normalized_command TEXT NOT NULL DEFAULT '',
			call_id TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			reasoning_output_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			model_context_window INTEGER NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '',
			raw_json TEXT NOT NULL DEFAULT ''
		)`,
		`ALTER TABLE payload_events ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE payload_events ADD COLUMN content_bytes INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE payload_events ADD COLUMN role TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE payload_events ADD COLUMN input_text_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE payload_events ADD COLUMN input_text_bytes INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS payload_events_timestamp_idx ON payload_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS payload_events_session_idx ON payload_events(session_id)`,
		`CREATE INDEX IF NOT EXISTS payload_events_source_path_idx ON payload_events(source_path)`,
		`CREATE INDEX IF NOT EXISTS payload_events_type_idx ON payload_events(top_level_type, payload_type, phase)`,
		`CREATE INDEX IF NOT EXISTS payload_events_normalized_command_idx ON payload_events(normalized_command)`,
		`CREATE TABLE IF NOT EXISTS model_credit_rates (
			model TEXT PRIMARY KEY,
			input_credits_per_million REAL NOT NULL,
			cached_input_credits_per_million REAL NOT NULL,
			output_credits_per_million REAL NOT NULL,
			reasoning_credits_per_million REAL NOT NULL
		)`,
		`INSERT OR IGNORE INTO model_credit_rates (model, input_credits_per_million, cached_input_credits_per_million, output_credits_per_million, reasoning_credits_per_million) VALUES
			('codex-1', 1.0, 0.25, 4.0, 4.0),
			('gpt-5.3-codex', 1.0, 0.25, 4.0, 4.0),
			('gpt-5.3-codex-spark', 1.0, 0.25, 4.0, 4.0),
			('gpt-5.4-codex', 1.0, 0.25, 4.0, 4.0),
			('gpt-5.5', 1.0, 0.25, 4.0, 4.0),
			('gpt-5.5-codex', 1.0, 0.25, 4.0, 4.0),
			('unknown', 1.0, 0.25, 4.0, 4.0)`,
	}
	for _, stmt := range statements {
		if _, err := db.sql.ExecContext(ctx, stmt); err != nil {
			if isDuplicateColumnError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (db *DB) SourceFile(ctx context.Context, path string) (SourceFile, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT
		path,
		size_bytes,
		mod_time_unix,
		processed_offset,
		session_id,
		session_dir,
		model,
		function_call_count,
		payload_metrics_version,
		COALESCE(started_at, ''),
		COALESCE(last_seen_at, ''),
		last_total_input_tokens,
		last_total_cached_input_tokens,
		last_total_output_tokens,
		last_total_reasoning_tokens,
		last_total_tokens
		FROM source_files WHERE path = ?`, path)
	var sf SourceFile
	if err := row.Scan(
		&sf.Path,
		&sf.SizeBytes,
		&sf.ModTimeUnix,
		&sf.ProcessedOffset,
		&sf.SessionID,
		&sf.SessionDir,
		&sf.Model,
		&sf.FunctionCallCount,
		&sf.PayloadMetricsVersion,
		&sf.StartedAt,
		&sf.LastSeenAt,
		&sf.LastTotalInputTokens,
		&sf.LastTotalCachedInputTokens,
		&sf.LastTotalOutputTokens,
		&sf.LastTotalReasoningTokens,
		&sf.LastTotalTokens,
	); err != nil {
		if err == sql.ErrNoRows {
			return SourceFile{}, false, nil
		}
		return SourceFile{}, false, err
	}
	return sf, true, nil
}

func (db *DB) HasPayloadEvents(ctx context.Context, path string) (bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM payload_events WHERE source_path = ? LIMIT 1)`, path)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM command_events WHERE source_path = ?`, path); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM payload_events WHERE source_path = ?`, path); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_files WHERE path = ?`, path); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) SessionSourcePaths(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT path FROM source_files WHERE session_id = ? ORDER BY path`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func (db *DB) DeleteSession(ctx context.Context, sessionID string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_events WHERE session_id = ?`, sessionID); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM command_events WHERE session_id = ?`, sessionID); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM payload_events WHERE session_id = ?`, sessionID); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_files WHERE session_id = ?`, sessionID); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) SaveFileSync(ctx context.Context, source SourceFile, events []TokenEvent) error {
	return db.SaveFileSyncWithDetails(ctx, source, events, nil, nil)
}

func (db *DB) SaveFileSyncWithCommands(ctx context.Context, source SourceFile, events []TokenEvent, commands []CommandEvent) error {
	return db.SaveFileSyncWithDetails(ctx, source, events, commands, nil)
}

func (db *DB) SaveFileSyncWithDetails(ctx context.Context, source SourceFile, events []TokenEvent, commands []CommandEvent, payloads []PayloadEvent) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO token_events (
			session_id, source_path, timestamp, input_tokens, cached_input_tokens, output_tokens,
			reasoning_output_tokens, total_tokens, model_context_window, model
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.SessionID,
			event.SourcePath,
			event.Timestamp,
			event.InputTokens,
			event.CachedInputTokens,
			event.OutputTokens,
			event.ReasoningOutputTokens,
			event.TotalTokens,
			event.ModelContextWindow,
			event.Model,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, command := range commands {
		if _, err := tx.ExecContext(ctx, `INSERT INTO command_events (
			session_id, source_path, timestamp, event_type, command_name, session_dir
		) VALUES (?, ?, ?, ?, ?, ?)`,
			command.SessionID,
			command.SourcePath,
			command.Timestamp,
			command.EventType,
			command.CommandName,
			command.SessionDir,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, payload := range payloads {
		if _, err := tx.ExecContext(ctx, `INSERT INTO payload_events (
			session_id, source_path, timestamp, top_level_type, payload_type, phase, payload_bytes,
			content_bytes, role, input_text_count, input_text_bytes,
			completed_at, duration_ms, time_to_first_token_ms, command_name, normalized_command, call_id,
			input_tokens, cached_input_tokens, output_tokens, reasoning_output_tokens, total_tokens,
			model_context_window, model, payload_json, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			payload.SessionID,
			payload.SourcePath,
			payload.Timestamp,
			payload.TopLevelType,
			payload.PayloadType,
			payload.Phase,
			payload.PayloadBytes,
			payload.ContentBytes,
			payload.Role,
			payload.InputTextCount,
			payload.InputTextBytes,
			payload.CompletedAt,
			payload.DurationMS,
			payload.TimeToFirstTokenMS,
			payload.CommandName,
			payload.NormalizedCommand,
			payload.CallID,
			payload.InputTokens,
			payload.CachedInputTokens,
			payload.OutputTokens,
			payload.ReasoningOutputTokens,
			payload.TotalTokens,
			payload.ModelContextWindow,
			payload.Model,
			payload.PayloadJSON,
			payload.RawJSON,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_files (
		path,
		size_bytes,
		mod_time_unix,
		processed_offset,
		session_id,
		session_dir,
		model,
		function_call_count,
		payload_metrics_version,
		started_at,
		last_seen_at,
		last_total_input_tokens,
		last_total_cached_input_tokens,
		last_total_output_tokens,
		last_total_reasoning_tokens,
		last_total_tokens
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(path) DO UPDATE SET
		size_bytes = excluded.size_bytes,
		mod_time_unix = excluded.mod_time_unix,
		processed_offset = excluded.processed_offset,
		session_id = excluded.session_id,
		session_dir = COALESCE(NULLIF(excluded.session_dir, ''), source_files.session_dir),
		model = COALESCE(NULLIF(excluded.model, ''), source_files.model),
		function_call_count = excluded.function_call_count,
		payload_metrics_version = excluded.payload_metrics_version,
		started_at = COALESCE(NULLIF(source_files.started_at, ''), excluded.started_at),
		last_seen_at = excluded.last_seen_at,
		last_total_input_tokens = excluded.last_total_input_tokens,
		last_total_cached_input_tokens = excluded.last_total_cached_input_tokens,
		last_total_output_tokens = excluded.last_total_output_tokens,
		last_total_reasoning_tokens = excluded.last_total_reasoning_tokens,
		last_total_tokens = excluded.last_total_tokens`,
		source.Path,
		source.SizeBytes,
		source.ModTimeUnix,
		source.ProcessedOffset,
		source.SessionID,
		source.SessionDir,
		source.Model,
		source.FunctionCallCount,
		source.PayloadMetricsVersion,
		source.StartedAt,
		source.LastSeenAt,
		source.LastTotalInputTokens,
		source.LastTotalCachedInputTokens,
		source.LastTotalOutputTokens,
		source.LastTotalReasoningTokens,
		source.LastTotalTokens,
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
	rows, err := db.sql.QueryContext(ctx, `SELECT id, session_id, source_path, timestamp, input_tokens, cached_input_tokens, output_tokens, reasoning_output_tokens, total_tokens, COALESCE(model_context_window, 0), COALESCE(model, '') FROM token_events ORDER BY timestamp DESC, id DESC`)
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
			&event.Model,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

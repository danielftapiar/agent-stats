# agent-stats

`agent-stats` is a Go CLI for inspecting local Codex usage without calling an API or uploading prompt content. Codex stores local session rollouts under `~/.codex/sessions/`; those rollouts include `token_count` events containing token usage totals for a session.

The goal of this project is to turn those local usage events into a fast, readable terminal dashboard that shows where tokens are going over time.

## What It Reads

Codex emits `token_count` events with cumulative totals similar to:

```json
{
  "type": "token_count",
  "info": {
    "total_token_usage": {
      "input_tokens": 0,
      "cached_input_tokens": 0,
      "output_tokens": 0,
      "reasoning_output_tokens": 0
    }
  }
}
```

Because the values are cumulative within a session, `agent-stats` should calculate usage by taking the delta between consecutive `token_count` events instead of summing every event directly. Repeated `token_count` events with the same cumulative totals should be skipped so resumed or duplicated Codex events do not double-count usage.

## Features

- Scan `~/.codex/sessions/` for local Codex session logs.
- Parse JSONL rollout files without reading or displaying prompt or response text.
- Aggregate token usage by day and session.
- Track:
  - input tokens
  - cached input tokens
  - output tokens
  - reasoning output tokens
  - total tokens
- Calculate prompt cache hit rate:

```text
hit_rate = cache_read / (cache_read + cache_creation + uncached_input)
```

For local Codex logs, `cached_input_tokens` is treated as `cache_read`, `input_tokens - cached_input_tokens` is treated as `uncached_input` when possible, and `cache_creation` is currently `0` because Codex rollouts do not expose a separate cache creation field.

- Render a fast terminal graph UI for quick inspection.
- Support a quiet machine-readable mode for scripts.

## CLI Design

Example commands:

```bash
agent-stats summary
agent-stats today
agent-stats graph
agent-stats graph --since 7d
agent-stats sessions --limit 20
agent-stats commands --limit 20
agent-stats payload
agent-stats payload <session-id>
agent-stats export --format json
```

## Views

The CLI should support a small set of useful views over the same parsed token data. Each view should make the grouping explicit so users can quickly understand whether token usage is driven by time, individual sessions, or cache behavior.

Useful views:

- `summary`: weekly credit spend, cache hit rate, function-call totals, and budget progress.
- `today`: current-day session totals with a custom time-block token graph and time-block drilldown.
- `sessions`: usage grouped by Codex session, sorted by latest activity, with model and credits.
- `commands`: function calls grouped by command name, with call count, session count, directory count, and first/last seen timestamps.
- `payload`: payload events grouped by top-level type, payload type, and phase. When opened for a session, it shows session timing rollups and token usage per interaction.
- `tokens`: input, cached input, output, and reasoning output grouped together for direct comparison.
- `top`: highest-usage sessions grouped by total tokens.

Useful group-by combinations:

| View | Primary group | Secondary group | Useful for |
| --- | --- | --- | --- |
| `summary` | Monday week start | budget/cache/function calls | Understanding weekly spend and cache efficiency |
| `today` | session | token graph | Spotting current-day heavy sessions |
| `sessions` | session | token type | Finding expensive sessions |
| `commands` | command name | function call metadata | Finding the tools and shell commands used most often |
| `payload` | top-level type/payload type/phase | payload size and timing | Inspecting local Codex event shapes for later analysis |
| `payload <session-id>` | interaction timestamp | token usage and payload timing | Drilling into one session's interaction-level cost |
| `top --by total` | session | total tokens | Ranking the biggest sessions |
| `top --by cached` | session | cached input tokens | Seeing where cache reuse is high |
| `top --by output` | session | output tokens | Finding output-heavy sessions |

The graph UI should make these groupings quick to switch between, ideally using keyboard shortcuts or a compact selector rather than requiring users to re-run commands for every comparison.

## Interactive UI

The CLI should include an interactive terminal mode with a lightweight command prompt and tabbed views.

Start interactive mode:

```bash
agent-stats
agent-stats tui
```

Interactive mode should support colon commands for switching views:

```text
:summary
:today
:sessions
:commands
:payload
:tokens
:top
:help
:quit
```

The same views should also be available as numbered tabs across the top of the UI:

```text
1 Summary  2 Today  3 Sessions  4 Commands  5 Payload  6 Tokens  7 Top
```

Keyboard behavior:

- Press `:` to focus the command prompt.
- Type a view command such as `:today` and press `Enter` to switch views.
- Press `1` through `9` to switch directly to the first nine tabs.
- Press `Tab` and `Shift+Tab` to move to the next or previous view, including tabs beyond `9`.
- In `sessions`, use `j`/`k` to select a session and `Enter` to open its `payload` drilldown.
- Press `?` to show available commands.
- Press `q` or run `:quit` to exit.

The command prompt should be useful but unobtrusive. It should sit at the bottom of the screen, show validation errors inline, and keep the current view visible while entering commands.

The default experience should be snappy and terminal-native:

- Use streaming file reads where possible.
- Avoid loading prompt or response text into display structures.
- Keep aggregation simple and incremental.
- Prefer compact charts over verbose tables.
- Make the graph UI responsive on small terminal widths.
- Render token usage as labelled terminal bar graphs.

The terminal UI is built with:

- `github.com/charmbracelet/bubbletea` for the interactive TUI loop.
- `github.com/charmbracelet/lipgloss` for styling.

## Build

Requirements:

- Go 1.25 or newer.

Build the CLI:

```bash
go build -o bin/agent-stats ./cmd/agent-stats
```

Run it locally:

```bash
./bin/agent-stats summary
```

Configure the weekly credit budget shown in summary progress bars:

```bash
AGENT_STATS_WEEKLY_CREDIT_BUDGET=5000 ./bin/agent-stats summary
```

If unset or invalid, the budget defaults to `10000` credits per week.

Install it into your `GOBIN`:

```bash
go install ./cmd/agent-stats
```

## Tests

Run all tests:

```bash
go test ./...
```

Run tests with the race detector:

```bash
go test -race ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

Suggested test coverage:

- JSONL parsing with valid, empty, and malformed lines.
- Delta calculation for cumulative `token_count` events.
- Session boundary handling.
- Aggregation by day.
- Cache hit rate calculation.
- TUI graph rendering with narrow and wide terminal sizes.

## Pre-Hooks

Start with a local pre-hook that formats all Go files before committing or running checks. More hooks can be added here as the project grows.

Example `.githooks/pre-commit`:

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Formatting Go files..."
go fmt ./...

if ! git diff --quiet; then
  echo "go fmt changed files. Review and stage the formatting changes, then commit again."
  exit 1
fi

go test ./...
```

Enable it for the repository:

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit
```

## Privacy Model

`agent-stats` should only read token metadata. It should not print, store, export, or transmit prompt text, response text, file contents, or tool output from Codex sessions.

The useful metrics are available from token counts alone, so the CLI should keep the privacy boundary clear:

- Read local files only.
- Extract token usage fields only.
- Display aggregate usage only.
- Do not require an API key.
- Do not send data to a remote service.

## Implementation Notes

The parser should walk files under `~/.codex/sessions/`, decode each JSONL line, filter for `type == "token_count"`, and extract `info.total_token_usage`.

For each session:

1. Keep the previous cumulative token total.
2. For each new `token_count` event, subtract the previous total from the current total.
3. Add only the positive delta to the aggregate.
4. Replace the previous total with the current total.

This avoids double-counting because Codex records running totals rather than independent per-event usage.

Recent Codex logs may also include `info.last_token_usage`. When present, prefer `last_token_usage` because it already represents the per-event increment. Fall back to the cumulative delta method when `last_token_usage` is missing. In both cases, dedupe by comparing the cumulative `total_token_usage` checkpoint before counting an event.

## Data Storage and Loading

The app should avoid reparsing all raw Codex JSONL on every startup once the history grows. The current local session directory is already large enough to justify an index:

- Session path: `~/.codex/sessions/`
- Current shape observed locally: dated folders containing `rollout-*.jsonl`
- Current volume observed locally: about 70 JSONL files, 26 dated folders, 225 MB, and roughly 14k `token_count` events

Recommended approach:

1. On first run, perform a full scan of `~/.codex/sessions/`.
2. Extract only token metadata into a local cache.
3. On later startups, stat the source files and only process new or changed bytes.
4. During interactive mode, stream updates from active session files.
5. On quit, flush the in-memory aggregates and file checkpoints to disk.

Store the cache under:

```text
~/.cache/agent-stats/codex-usage.db
```

Use SQLite as the first storage backend. It is small, local, durable, easy to inspect, and good enough for this data size. It also avoids inventing a custom file format too early.

Suggested tables:

```sql
CREATE TABLE source_files (
  path TEXT PRIMARY KEY,
  size_bytes INTEGER NOT NULL,
  mod_time_unix INTEGER NOT NULL,
  processed_offset INTEGER NOT NULL,
  session_id TEXT NOT NULL,
  started_at TEXT,
  last_seen_at TEXT,
  last_total_input_tokens INTEGER NOT NULL DEFAULT 0,
  last_total_cached_input_tokens INTEGER NOT NULL DEFAULT 0,
  last_total_output_tokens INTEGER NOT NULL DEFAULT 0,
  last_total_reasoning_tokens INTEGER NOT NULL DEFAULT 0,
  last_total_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE token_events (
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
  model TEXT NOT NULL DEFAULT ''
);

CREATE TABLE command_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  source_path TEXT NOT NULL,
  timestamp TEXT NOT NULL,
  event_type TEXT NOT NULL,
  command_name TEXT NOT NULL,
  session_dir TEXT NOT NULL DEFAULT ''
);

CREATE TABLE payload_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  source_path TEXT NOT NULL,
  timestamp TEXT NOT NULL,
  top_level_type TEXT NOT NULL,
  payload_type TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT '',
  payload_bytes INTEGER NOT NULL,
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
);

CREATE TABLE model_credit_rates (
  model TEXT PRIMARY KEY,
  input_credits_per_million REAL NOT NULL,
  cached_input_credits_per_million REAL NOT NULL,
  output_credits_per_million REAL NOT NULL,
  reasoning_credits_per_million REAL NOT NULL
);

CREATE INDEX token_events_timestamp_idx ON token_events(timestamp);
CREATE INDEX token_events_session_idx ON token_events(session_id);
CREATE INDEX command_events_command_name_idx ON command_events(command_name);
CREATE INDEX payload_events_type_idx ON payload_events(top_level_type, payload_type, phase);
```

The `token_events` table should store per-event increments, not cumulative totals. That keeps every view simple:

- `summary`: group token and command totals by Monday week start, such as `2026 May 18th`.
- `today`: group the current day's token totals by `session_id`, render `00:00-08:00`, `08:00-18:00`, and `18:00-00:00` graph buckets, and allow drilling into a time-block-filtered sessions view.
- `sessions`: group by `session_id`.
- `commands`: group `command_events` by `command_name`.
- `payload`: group `payload_events` by event shape, or by token-count interaction within one session.
- `credits`: join token rows to `model_credit_rates` by model and apply the per-million-token relationship.

Startup algorithm:

1. Open the SQLite cache.
2. Walk `~/.codex/sessions/**/*.jsonl`.
3. For each file, compare path, size, and modified time with `source_files`.
4. If unchanged, skip it.
5. If new, parse from byte offset `0`.
6. If changed and larger, seek to `processed_offset` and parse only appended lines.
7. If changed and smaller, delete that file's cached events and reprocess from `0`.
8. Store `response_item` function calls in `command_events` with command name, timestamp, session, and session directory.
9. Store every JSONL payload in `payload_events`, including raw payload JSON, payload length, timing fields, token usage for `token_count`, model, and normalized shell command names.
10. Seed `model_credit_rates` for known Codex models. The table is local and editable if Codex changes credit accounting.
11. Update `source_files` in the same transaction as inserted events.

Interactive streaming algorithm:

1. Load the SQLite aggregates into memory on startup.
2. Identify active files as files modified recently or files whose size changed during startup.
3. Poll active files on a short interval, such as 1 second.
4. Read from the saved `processed_offset`.
5. Buffer partial trailing lines until the next poll.
6. Insert new token events and update the visible aggregates.

This gives the app a fast startup path while still staying live during a Codex session. For the current local data size, a full Go scan at startup would probably be acceptable, but the indexed approach will feel better once historical logs become hundreds of megabytes or more.

## Issues

Implementation notes that changed or clarified the initial plan:

- The default cache path uses Go's `os.UserCacheDir()`. On macOS this resolves under `~/Library/Caches/agent-stats/` instead of the originally documented `~/.cache/agent-stats/`.
- The module currently targets Go 1.25 because that is the local toolchain used to resolve and verify dependencies.
- Current Codex logs include `payload.info.last_token_usage`, so the implementation prefers that over cumulative delta calculation. Cumulative delta fallback is still implemented for older log shapes.
- Incremental byte-offset parsing stores the last cumulative `total_token_usage` checkpoint per file so appended Codex events can be deduped safely across restarts.
- The pre-hook uses `go fmt ./...` instead of `gofmt -w .` so it does not traverse local build/module caches or unrelated hidden directories.
- The first implementation uses polling for active files rather than filesystem notifications. This keeps the dependency surface smaller and is fast enough for the current data volume.
- The non-interactive `graph` command currently maps to the `today` graph view. A dedicated `--since` filter still needs to be added.

## Reference

Implementation details are based on the local Codex logging behavior described in:

- https://dev.to/newtorob/claude-code-and-codex-are-logging-your-token-usage-locally-here-is-how-to-read-it-580

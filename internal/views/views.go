package views

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/danieltapia/agent-stats/internal/store"
	"github.com/guptarohit/asciigraph"
	"github.com/muesli/termenv"
)

type Totals struct {
	InputTokens              int64   `json:"input_tokens"`
	CachedInputTokens        int64   `json:"cached_input_tokens"`
	UncachedInputTokens      int64   `json:"uncached_input_tokens"`
	CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	ReasoningOutputTokens    int64   `json:"reasoning_output_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	Credits                  float64 `json:"credits"`
	CacheHitRate             float64 `json:"cache_hit_rate"`
}

type Row struct {
	Label         string `json:"label"`
	PeriodStart   string `json:"period_start,omitempty"`
	Directory     string `json:"directory,omitempty"`
	Model         string `json:"model,omitempty"`
	EventType     string `json:"event_type,omitempty"`
	Phase         string `json:"phase,omitempty"`
	FunctionCalls int64  `json:"function_calls,omitempty"`
	SessionCount  int64  `json:"session_count,omitempty"`
	Count         int64  `json:"count,omitempty"`
	PayloadBytes  int64  `json:"payload_bytes,omitempty"`
	AvgBytes      int64  `json:"avg_bytes,omitempty"`
	MaxBytes      int64  `json:"max_bytes,omitempty"`
	AvgDurationMS int64  `json:"avg_duration_ms,omitempty"`
	MaxDurationMS int64  `json:"max_duration_ms,omitempty"`
	AvgTTFTMS     int64  `json:"avg_time_to_first_token_ms,omitempty"`
	FirstSeen     string `json:"first_seen,omitempty"`
	LastSeen      string `json:"last_seen,omitempty"`
	Totals        Totals `json:"totals"`
}

type Data struct {
	View          string `json:"view"`
	Period        string `json:"period,omitempty"`
	PeriodStart   string `json:"period_start,omitempty"`
	Session       string `json:"session,omitempty"`
	Interaction   string `json:"interaction,omitempty"`
	SelectedIndex int    `json:"selected_index,omitempty"`
	Summary       []Row  `json:"summary,omitempty"`
	GraphRows     []Row  `json:"graph_rows,omitempty"`
	Totals        Totals `json:"totals"`
	Rows          []Row  `json:"rows"`
}

const selectedRowMarker = "\x1f"

func Load(ctx context.Context, db *store.DB, view string, limit int, now time.Time) (Data, error) {
	var (
		rows []Row
		err  error
	)
	switch view {
	case "summary":
		rows, err = queryWeeklySummary(ctx, db)
	case "tokens":
		rows, err = queryGrouped(ctx, db, "token_type", "", 0)
		if err == nil {
			totals, totalsErr := queryTotals(ctx, db, "")
			if totalsErr != nil {
				return Data{}, totalsErr
			}
			return Data{View: view, SelectedIndex: -1, Totals: totals, Rows: rows}, nil
		}
	case "today":
		start := now.Format("2006-01-02")
		rows, err = queryGrouped(ctx, db, "session_id", "timestamp >= ?", limitOrDefault(limit), start)
		if err != nil {
			return Data{}, err
		}
		graphRows, err := queryTodayHourlyGraph(ctx, db, start)
		if err != nil {
			return Data{}, err
		}
		data := Data{View: view, Rows: rows, GraphRows: graphRows}
		data.Totals = sumRows(rows)
		data.Totals.CacheHitRate = cacheHitRate(data.Totals)
		return data, nil
	case "sessions":
		rows, err = queryGrouped(ctx, db, "session_id", "", limitOrDefault(limit))
	case "top":
		rows, err = queryGrouped(ctx, db, "session_id", "", limitOrDefault(limit))
	case "commands":
		rows, err = queryCommands(ctx, db, limitOrDefault(limit))
	case "payload":
		rows, err = queryPayloadSummary(ctx, db, limitOrDefault(limit))
	default:
		rows, err = queryGrouped(ctx, db, "substr(timestamp, 1, 10)", "", 0)
	}
	if err != nil {
		return Data{}, err
	}
	data := Data{View: view, SelectedIndex: -1, Rows: rows}
	for _, row := range rows {
		data.Totals = addTotals(data.Totals, row.Totals)
	}
	data.Totals.CacheHitRate = cacheHitRate(data.Totals)
	return data, nil
}

func LoadSessionsForDay(ctx context.Context, db *store.DB, day string, limit int) (Data, error) {
	rows, err := queryGrouped(ctx, db, "session_id", "timestamp >= ? AND timestamp < date(?, '+1 day')", limitOrDefault(limit), day, day)
	if err != nil {
		return Data{}, err
	}
	data := Data{View: "sessions", Period: "day", PeriodStart: day, SelectedIndex: -1, Rows: rows}
	data.Totals = sumRows(rows)
	data.Totals.CacheHitRate = cacheHitRate(data.Totals)
	return data, nil
}

func LoadSessionPayload(ctx context.Context, db *store.DB, sessionID string, limit int) (Data, error) {
	rows, err := querySessionInteractions(ctx, db, sessionID, limitOrDefault(limit))
	if err != nil {
		return Data{}, err
	}
	summary, err := querySessionPayloadSummary(ctx, db, sessionID)
	if err != nil {
		return Data{}, err
	}
	data := Data{View: "payload", Session: sessionID, SelectedIndex: -1, Summary: summary, Rows: rows}
	for _, row := range rows {
		data.Totals = addTotals(data.Totals, row.Totals)
	}
	data.Totals.CacheHitRate = cacheHitRate(data.Totals)
	return data, nil
}

func LoadPayloadInteraction(ctx context.Context, db *store.DB, sessionID, interaction string) (Data, error) {
	summary, err := queryInteractionSummary(ctx, db, sessionID, interaction)
	if err != nil {
		return Data{}, err
	}
	return Data{View: "payload", Session: sessionID, Interaction: interaction, SelectedIndex: -1, Summary: summary}, nil
}

func LoadSummaryWeek(ctx context.Context, db *store.DB, weekStart string) (Data, error) {
	rows, err := queryDailySummaryForWeek(ctx, db, weekStart)
	if err != nil {
		return Data{}, err
	}
	data := Data{View: "summary", Period: "day", PeriodStart: weekStart, SelectedIndex: -1, Rows: rows}
	for _, row := range rows {
		data.Totals = addTotals(data.Totals, row.Totals)
	}
	data.Totals.CacheHitRate = cacheHitRate(data.Totals)
	return data, nil
}

const uncachedInputExpr = `(CASE
	WHEN token_events.input_tokens >= token_events.cached_input_tokens THEN token_events.input_tokens - token_events.cached_input_tokens
	ELSE token_events.input_tokens
END)`

const creditExpr = `(` + uncachedInputExpr + ` * COALESCE(rate.input_credits_per_million, fallback.input_credits_per_million) +
	token_events.cached_input_tokens * COALESCE(rate.cached_input_credits_per_million, fallback.cached_input_credits_per_million) +
	token_events.output_tokens * COALESCE(rate.output_credits_per_million, fallback.output_credits_per_million) +
	token_events.reasoning_output_tokens * COALESCE(rate.reasoning_credits_per_million, fallback.reasoning_credits_per_million)) / 1000000.0`

const creditJoins = ` LEFT JOIN model_credit_rates rate ON rate.model = COALESCE(NULLIF(token_events.model, ''), 'unknown')
	LEFT JOIN model_credit_rates fallback ON fallback.model = 'unknown'`

func queryWeeklySummary(ctx context.Context, db *store.DB) ([]Row, error) {
	query := `WITH weekly_tokens AS (
		SELECT date(timestamp, '-6 days', 'weekday 1') AS label,
			SUM(input_tokens) AS input_tokens,
			SUM(cached_input_tokens) AS cached_input_tokens,
			SUM(output_tokens) AS output_tokens,
			SUM(reasoning_output_tokens) AS reasoning_output_tokens,
			SUM(total_tokens) AS total_tokens,
			SUM(` + creditExpr + `) AS credits
		FROM token_events` + creditJoins + `
		GROUP BY label
	),
	weekly_calls AS (
		SELECT date(timestamp, '-6 days', 'weekday 1') AS label, COUNT(*) AS function_calls
		FROM command_events
		GROUP BY label
	)
	SELECT weekly_tokens.label,
		weekly_tokens.input_tokens,
		weekly_tokens.cached_input_tokens,
		weekly_tokens.output_tokens,
		weekly_tokens.reasoning_output_tokens,
		weekly_tokens.total_tokens,
		weekly_tokens.credits,
		COALESCE(weekly_calls.function_calls, 0)
	FROM weekly_tokens
	LEFT JOIN weekly_calls ON weekly_calls.label = weekly_tokens.label
	ORDER BY weekly_tokens.label DESC`
	sqlRows, err := db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var rows []Row
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(
			&row.Label,
			&row.Totals.InputTokens,
			&row.Totals.CachedInputTokens,
			&row.Totals.OutputTokens,
			&row.Totals.ReasoningOutputTokens,
			&row.Totals.TotalTokens,
			&row.Totals.Credits,
			&row.FunctionCalls,
		); err != nil {
			return nil, err
		}
		row.PeriodStart = row.Label
		row.Label = formatDateLabel(row.Label)
		row.Totals = withDerived(row.Totals)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryDailySummaryForWeek(ctx context.Context, db *store.DB, weekStart string) ([]Row, error) {
	query := `WITH daily_tokens AS (
		SELECT substr(timestamp, 1, 10) AS label,
			SUM(input_tokens) AS input_tokens,
			SUM(cached_input_tokens) AS cached_input_tokens,
			SUM(output_tokens) AS output_tokens,
			SUM(reasoning_output_tokens) AS reasoning_output_tokens,
			SUM(total_tokens) AS total_tokens,
			SUM(` + creditExpr + `) AS credits
		FROM token_events` + creditJoins + `
		WHERE timestamp >= ? AND timestamp < date(?, '+7 days')
		GROUP BY label
	),
	daily_calls AS (
		SELECT substr(timestamp, 1, 10) AS label, COUNT(*) AS function_calls
		FROM command_events
		WHERE timestamp >= ? AND timestamp < date(?, '+7 days')
		GROUP BY label
	)
	SELECT daily_tokens.label,
		daily_tokens.input_tokens,
		daily_tokens.cached_input_tokens,
		daily_tokens.output_tokens,
		daily_tokens.reasoning_output_tokens,
		daily_tokens.total_tokens,
		daily_tokens.credits,
		COALESCE(daily_calls.function_calls, 0)
	FROM daily_tokens
	LEFT JOIN daily_calls ON daily_calls.label = daily_tokens.label
	ORDER BY daily_tokens.label DESC`
	sqlRows, err := db.Query(ctx, query, weekStart, weekStart, weekStart, weekStart)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var rows []Row
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(
			&row.Label,
			&row.Totals.InputTokens,
			&row.Totals.CachedInputTokens,
			&row.Totals.OutputTokens,
			&row.Totals.ReasoningOutputTokens,
			&row.Totals.TotalTokens,
			&row.Totals.Credits,
			&row.FunctionCalls,
		); err != nil {
			return nil, err
		}
		row.PeriodStart = row.Label
		row.Label = formatDateLabel(row.Label)
		row.Totals = withDerived(row.Totals)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryTodayHourlyGraph(ctx context.Context, db *store.DB, day string) ([]Row, error) {
	query := `WITH RECURSIVE hours(hour) AS (
		SELECT 0
		UNION ALL
		SELECT hour + 1 FROM hours WHERE hour < 23
	),
	hourly AS (
		SELECT CAST(strftime('%H', timestamp) AS INTEGER) AS hour,
			SUM(total_tokens) AS total_tokens
		FROM token_events
		WHERE timestamp >= ? AND timestamp < date(?, '+1 day')
		GROUP BY hour
	)
	SELECT CASE WHEN hours.hour = 23 THEN '23:59' ELSE printf('%02d:00', hours.hour) END,
		COALESCE(hourly.total_tokens, 0)
	FROM hours
	LEFT JOIN hourly ON hourly.hour = hours.hour
	ORDER BY hours.hour`
	sqlRows, err := db.Query(ctx, query, day, day)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	rows := make([]Row, 0, 24)
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(&row.Label, &row.Totals.TotalTokens); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryPayloadSummary(ctx context.Context, db *store.DB, limit int) ([]Row, error) {
	query := `SELECT top_level_type || '/' || payload_type AS label,
		phase,
		COUNT(*),
		SUM(payload_bytes),
		CAST(AVG(payload_bytes) AS INTEGER),
		MAX(payload_bytes),
		CAST(AVG(duration_ms) AS INTEGER),
		MAX(duration_ms),
		CAST(AVG(time_to_first_token_ms) AS INTEGER)
		FROM payload_events
		GROUP BY top_level_type, payload_type, phase
		ORDER BY COUNT(*) DESC, MAX(timestamp) DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	sqlRows, err := db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var rows []Row
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(&row.Label, &row.Phase, &row.Count, &row.PayloadBytes, &row.AvgBytes, &row.MaxBytes, &row.AvgDurationMS, &row.MaxDurationMS, &row.AvgTTFTMS); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func querySessionPayloadSummary(ctx context.Context, db *store.DB, sessionID string) ([]Row, error) {
	rows := []Row{}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "duration", `SELECT 'session duration', '', 1, 0, 0, 0, 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ?`); err != nil {
		return nil, err
	} else {
		row.AvgDurationMS = diffMillis(row.FirstSeen, row.LastSeen)
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "prompt-final", `SELECT 'prompt to final answer', '', 1, 0, 0, 0, 0, 0, 0, COALESCE(MIN(CASE WHEN top_level_type = 'event_msg' THEN timestamp END), ''), COALESCE(MAX(CASE WHEN top_level_type = 'response_item' AND payload_type = 'message' THEN timestamp END), '') FROM payload_events WHERE session_id = ?`); err != nil {
		return nil, err
	} else {
		row.AvgDurationMS = diffMillis(row.FirstSeen, row.LastSeen)
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "response_item timing", `SELECT 'response_item timing', '', COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), COALESCE(CAST(AVG(duration_ms) AS INTEGER), 0), COALESCE(MAX(duration_ms), 0), COALESCE(CAST(AVG(time_to_first_token_ms) AS INTEGER), 0), COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ? AND top_level_type = 'response_item'`); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "payload by phase", `SELECT 'payload bytes by phase', phase, COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ? GROUP BY phase ORDER BY SUM(payload_bytes) DESC LIMIT 1`); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "session_meta bytes", `SELECT 'session_meta bytes', '', COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ? AND top_level_type = 'session_meta'`); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "event_msg count", `SELECT 'event_msg count', '', COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ? AND top_level_type = 'event_msg'`); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetric(ctx, db, sessionID, "input_text", `SELECT 'input_text', '', COALESCE(SUM(input_text_count), 0), COALESCE(SUM(input_text_bytes), 0), COALESCE(CAST(AVG(input_text_bytes) AS INTEGER), 0), COALESCE(MAX(input_text_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ? AND input_text_count > 0`); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	commandRows, err := queryTopCommands(ctx, db, sessionID, "", "", 5)
	if err != nil {
		return nil, err
	}
	rows = append(rows, commandRows...)
	roleRows, err := queryRoleCounts(ctx, db, sessionID, "", "")
	if err != nil {
		return nil, err
	}
	rows = append(rows, roleRows...)
	return rows, nil
}

func queryTopCommands(ctx context.Context, db *store.DB, sessionID, startAfter, endAt string, limit int) ([]Row, error) {
	where := "session_id = ? AND payload_type = 'function_call' AND COALESCE(NULLIF(normalized_command, ''), command_name) != ''"
	args := []any{sessionID}
	if endAt != "" {
		where += " AND timestamp <= ?"
		args = append(args, endAt)
	}
	if startAfter != "" {
		where += " AND timestamp > ?"
		args = append(args, startAfter)
	}
	query := `SELECT 'top command', COALESCE(NULLIF(normalized_command, ''), command_name), COUNT(*), 0, 0, 0, 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '')
		FROM payload_events
		WHERE ` + where + `
		GROUP BY COALESCE(NULLIF(normalized_command, ''), command_name)
		ORDER BY COUNT(*) DESC, COALESCE(NULLIF(normalized_command, ''), command_name) ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	sqlRows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var rows []Row
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(&row.Label, &row.Phase, &row.Count, &row.PayloadBytes, &row.AvgBytes, &row.MaxBytes, &row.AvgDurationMS, &row.MaxDurationMS, &row.AvgTTFTMS, &row.FirstSeen, &row.LastSeen); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryRoleCounts(ctx context.Context, db *store.DB, sessionID, startAfter, endAt string) ([]Row, error) {
	where := "session_id = ? AND role != ''"
	args := []any{sessionID}
	if endAt != "" {
		where += " AND timestamp <= ?"
		args = append(args, endAt)
	}
	if startAfter != "" {
		where += " AND timestamp > ?"
		args = append(args, startAfter)
	}
	sqlRows, err := db.Query(ctx, `SELECT 'role count', role, COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '')
		FROM payload_events
		WHERE `+where+`
		GROUP BY role
		ORDER BY COUNT(*) DESC, role ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var rows []Row
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(&row.Label, &row.Phase, &row.Count, &row.PayloadBytes, &row.AvgBytes, &row.MaxBytes, &row.AvgDurationMS, &row.MaxDurationMS, &row.AvgTTFTMS, &row.FirstSeen, &row.LastSeen); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryInteractionSummary(ctx context.Context, db *store.DB, sessionID, interaction string) ([]Row, error) {
	previous, err := previousInteraction(ctx, db, sessionID, interaction)
	if err != nil {
		return nil, err
	}
	where := "session_id = ? AND timestamp <= ?"
	args := []any{sessionID, interaction}
	if previous != "" {
		where += " AND timestamp > ?"
		args = append(args, previous)
	}
	rows := []Row{}
	if row, err := singlePayloadMetricArgs(ctx, db, "interaction payload", `SELECT 'interaction payload', '', COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), COALESCE(CAST(AVG(duration_ms) AS INTEGER), 0), COALESCE(MAX(duration_ms), 0), COALESCE(CAST(AVG(time_to_first_token_ms) AS INTEGER), 0), COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE `+where, args...); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetricArgs(ctx, db, "event_msg count", `SELECT 'event_msg count', '', COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(CAST(AVG(payload_bytes) AS INTEGER), 0), COALESCE(MAX(payload_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE top_level_type = 'event_msg' AND `+where, args...); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	if row, err := singlePayloadMetricArgs(ctx, db, "input_text", `SELECT 'input_text', '', COALESCE(SUM(input_text_count), 0), COALESCE(SUM(input_text_bytes), 0), COALESCE(CAST(AVG(input_text_bytes) AS INTEGER), 0), COALESCE(MAX(input_text_bytes), 0), 0, 0, 0, COALESCE(MIN(timestamp), ''), COALESCE(MAX(timestamp), '') FROM payload_events WHERE input_text_count > 0 AND `+where, args...); err != nil {
		return nil, err
	} else {
		rows = append(rows, row)
	}
	commandRows, err := queryTopCommands(ctx, db, sessionID, previous, interaction, 5)
	if err != nil {
		return nil, err
	}
	rows = append(rows, commandRows...)
	roleRows, err := queryRoleCounts(ctx, db, sessionID, previous, interaction)
	if err != nil {
		return nil, err
	}
	rows = append(rows, roleRows...)
	return rows, nil
}

func previousInteraction(ctx context.Context, db *store.DB, sessionID, interaction string) (string, error) {
	rows, err := db.Query(ctx, `SELECT COALESCE(MAX(timestamp), '') FROM payload_events WHERE session_id = ? AND payload_type = 'token_count' AND timestamp < ?`, sessionID, interaction)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var previous string
	if rows.Next() {
		if err := rows.Scan(&previous); err != nil {
			return "", err
		}
	}
	return previous, rows.Err()
}

func singlePayloadMetricArgs(ctx context.Context, db *store.DB, label, query string, args ...any) (Row, error) {
	sqlRows, err := db.Query(ctx, query, args...)
	if err != nil {
		return Row{}, err
	}
	defer sqlRows.Close()
	row := Row{Label: label}
	if sqlRows.Next() {
		if err := sqlRows.Scan(&row.Label, &row.Phase, &row.Count, &row.PayloadBytes, &row.AvgBytes, &row.MaxBytes, &row.AvgDurationMS, &row.MaxDurationMS, &row.AvgTTFTMS, &row.FirstSeen, &row.LastSeen); err != nil {
			return Row{}, err
		}
	}
	return row, sqlRows.Err()
}

func singlePayloadMetric(ctx context.Context, db *store.DB, sessionID, label, query string) (Row, error) {
	sqlRows, err := db.Query(ctx, query, sessionID)
	if err != nil {
		return Row{}, err
	}
	defer sqlRows.Close()
	row := Row{Label: label}
	if sqlRows.Next() {
		if err := sqlRows.Scan(&row.Label, &row.Phase, &row.Count, &row.PayloadBytes, &row.AvgBytes, &row.MaxBytes, &row.AvgDurationMS, &row.MaxDurationMS, &row.AvgTTFTMS, &row.FirstSeen, &row.LastSeen); err != nil {
			return Row{}, err
		}
	}
	return row, sqlRows.Err()
}

func querySessionInteractions(ctx context.Context, db *store.DB, sessionID string, limit int) ([]Row, error) {
	query := `SELECT timestamp,
		input_tokens, cached_input_tokens, output_tokens, reasoning_output_tokens, total_tokens,
		payload_bytes, duration_ms, time_to_first_token_ms
		FROM payload_events
		WHERE session_id = ? AND payload_type = 'token_count'
		ORDER BY timestamp DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	sqlRows, err := db.Query(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var rows []Row
	for sqlRows.Next() {
		var row Row
		if err := sqlRows.Scan(&row.Label, &row.Totals.InputTokens, &row.Totals.CachedInputTokens, &row.Totals.OutputTokens, &row.Totals.ReasoningOutputTokens, &row.Totals.TotalTokens, &row.PayloadBytes, &row.AvgDurationMS, &row.AvgTTFTMS); err != nil {
			return nil, err
		}
		row.Totals = withDerived(row.Totals)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryCommands(ctx context.Context, db *store.DB, limit int) ([]Row, error) {
	query := `SELECT command_name,
		MAX(event_type),
		COUNT(*),
		COUNT(DISTINCT session_id),
		COUNT(DISTINCT NULLIF(session_dir, '')),
		MIN(timestamp),
		MAX(timestamp)
		FROM command_events
		GROUP BY command_name
		ORDER BY MAX(timestamp) DESC, command_name ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	sqlRows, err := db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []Row
	for sqlRows.Next() {
		var row Row
		var directoryCount int64
		if err := sqlRows.Scan(
			&row.Label,
			&row.EventType,
			&row.FunctionCalls,
			&row.SessionCount,
			&directoryCount,
			&row.FirstSeen,
			&row.LastSeen,
		); err != nil {
			return nil, err
		}
		row.Directory = fmt.Sprintf("%d dirs", directoryCount)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryGrouped(ctx context.Context, db *store.DB, groupExpr, where string, limit int, args ...any) ([]Row, error) {
	if groupExpr == "token_type" {
		totals, err := queryTotals(ctx, db, where, args...)
		if err != nil {
			return nil, err
		}
		return []Row{
			{Label: "uncached input", Totals: withDerived(Totals{TotalTokens: totals.UncachedInputTokens, InputTokens: totals.UncachedInputTokens, Credits: tokenTypeCredits(ctx, db, "uncached_input_tokens", where, args...)})},
			{Label: "cache read", Totals: withDerived(Totals{TotalTokens: totals.CachedInputTokens, CachedInputTokens: totals.CachedInputTokens, Credits: tokenTypeCredits(ctx, db, "cached_input_tokens", where, args...)})},
			{Label: "cache creation", Totals: withDerived(Totals{})},
			{Label: "output", Totals: Totals{TotalTokens: totals.OutputTokens, OutputTokens: totals.OutputTokens, Credits: tokenTypeCredits(ctx, db, "output_tokens", where, args...)}},
			{Label: "reasoning output", Totals: Totals{TotalTokens: totals.ReasoningOutputTokens, ReasoningOutputTokens: totals.ReasoningOutputTokens, Credits: tokenTypeCredits(ctx, db, "reasoning_output_tokens", where, args...)}},
		}, nil
	}

	query := fmt.Sprintf(`SELECT %s AS label,
		SUM(input_tokens), SUM(cached_input_tokens), SUM(output_tokens),
		SUM(reasoning_output_tokens), SUM(total_tokens), SUM(`+creditExpr+`), MAX(timestamp)
		FROM token_events`+creditJoins, groupExpr)
	if groupExpr == "session_id" {
		query = `WITH source_meta AS (
			SELECT session_id, MAX(session_dir) AS session_dir, MAX(model) AS model, SUM(function_call_count) AS function_call_count
			FROM source_files
			GROUP BY session_id
		)
		SELECT token_events.session_id AS label,
		SUM(input_tokens), SUM(cached_input_tokens), SUM(output_tokens),
		SUM(reasoning_output_tokens), SUM(total_tokens),
		SUM(` + creditExpr + `),
		MAX(timestamp),
		COALESCE(source_meta.session_dir, ''), COALESCE(source_meta.model, ''), COALESCE(source_meta.function_call_count, 0)
		FROM token_events` + creditJoins + `
		LEFT JOIN source_meta ON source_meta.session_id = token_events.session_id`
	}
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY label"
	if groupExpr == "session_id" {
		query += ", source_meta.session_dir, source_meta.model, source_meta.function_call_count"
	}
	query += " ORDER BY MAX(timestamp) DESC"
	if groupExpr == "substr(timestamp, 1, 10)" || strings.Contains(groupExpr, "12, 2") {
		query = strings.Replace(query, "ORDER BY MAX(timestamp) DESC", "ORDER BY label DESC", 1)
	}
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	sqlRows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []Row
	for sqlRows.Next() {
		var row Row
		if groupExpr == "session_id" {
			if err := sqlRows.Scan(
				&row.Label,
				&row.Totals.InputTokens,
				&row.Totals.CachedInputTokens,
				&row.Totals.OutputTokens,
				&row.Totals.ReasoningOutputTokens,
				&row.Totals.TotalTokens,
				&row.Totals.Credits,
				&row.LastSeen,
				&row.Directory,
				&row.Model,
				&row.FunctionCalls,
			); err != nil {
				return nil, err
			}
		} else {
			if err := sqlRows.Scan(
				&row.Label,
				&row.Totals.InputTokens,
				&row.Totals.CachedInputTokens,
				&row.Totals.OutputTokens,
				&row.Totals.ReasoningOutputTokens,
				&row.Totals.TotalTokens,
				&row.Totals.Credits,
				&row.LastSeen,
			); err != nil {
				return nil, err
			}
		}
		row.Totals = withDerived(row.Totals)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryTotals(ctx context.Context, db *store.DB, where string, args ...any) (Totals, error) {
	query := `SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_output_tokens), 0), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(` + creditExpr + `), 0) FROM token_events` + creditJoins
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return Totals{}, err
	}
	defer rows.Close()
	var totals Totals
	if rows.Next() {
		if err := rows.Scan(&totals.InputTokens, &totals.CachedInputTokens, &totals.OutputTokens, &totals.ReasoningOutputTokens, &totals.TotalTokens, &totals.Credits); err != nil {
			return Totals{}, err
		}
	}
	return withDerived(totals), rows.Err()
}

func tokenTypeCredits(ctx context.Context, db *store.DB, column, where string, args ...any) float64 {
	rateColumn := map[string]string{
		"uncached_input_tokens":   "input_credits_per_million",
		"cached_input_tokens":     "cached_input_credits_per_million",
		"output_tokens":           "output_credits_per_million",
		"reasoning_output_tokens": "reasoning_credits_per_million",
	}[column]
	if rateColumn == "" {
		return 0
	}
	tokenExpr := "token_events." + column
	if column == "uncached_input_tokens" {
		tokenExpr = uncachedInputExpr
	}
	query := fmt.Sprintf(`SELECT COALESCE(SUM(%s * COALESCE(rate.%s, fallback.%s)) / 1000000.0, 0)
		FROM token_events%s`, tokenExpr, rateColumn, rateColumn, creditJoins)
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var credits float64
	if rows.Next() {
		if err := rows.Scan(&credits); err != nil {
			return 0
		}
	}
	return credits
}

func Render(data Data, view string) string {
	return RenderWithWidth(data, view, 0)
}

func RenderWithWidth(data Data, view string, width int) string {
	var b strings.Builder
	data.Totals = withDerived(data.Totals)
	fmt.Fprintf(&b, "%s\n\n", strings.ToUpper(view))
	if view == "commands" {
		writeCommandSummary(&b, data.Rows)
		if len(data.Rows) == 0 {
			b.WriteString("\nNo command events found.\n")
			return b.String()
		}
		b.WriteString("\n")
		writeCommandRows(&b, data.Rows, width)
		return b.String()
	}
	if view == "payload" {
		writePayloadSummary(&b, data, width)
		if data.Interaction != "" {
			return b.String()
		}
		if len(data.Rows) == 0 {
			b.WriteString("\nNo payload events found.\n")
			return b.String()
		}
		b.WriteString("\n")
		if data.Session != "" {
			writePayloadSessionRows(&b, data.Rows, data.SelectedIndex, width)
		} else {
			writePayloadRows(&b, data.Rows, width)
		}
		return b.String()
	}
	if view == "summary" {
		writeSummary(&b, data, width)
		return b.String()
	}
	writeTotals(&b, data.Totals)
	if len(data.Rows) == 0 {
		b.WriteString("\nNo token events found.\n")
		return b.String()
	}
	b.WriteString("\n")
	switch view {
	case "today":
		graphRows := data.GraphRows
		if len(graphRows) == 0 {
			graphRows = data.Rows
		}
		writeGraph(&b, graphRows, view, width)
		b.WriteString("\n")
	}
	if view == "sessions" && data.Period == "day" && data.PeriodStart != "" {
		fmt.Fprintf(&b, "Day: %s\n\n", formatDateLabel(data.PeriodStart))
	}
	writeRows(&b, data.Rows, view, data.SelectedIndex, width)
	return b.String()
}

func writeCommandSummary(b *strings.Builder, rows []Row) {
	var calls, sessions int64
	for _, row := range rows {
		calls += row.FunctionCalls
		sessions += row.SessionCount
	}
	fmt.Fprintf(b, "Commands: %d  Calls: %s  Session refs: %s\n", len(rows), formatInt(calls), formatInt(sessions))
}

func writeSummary(b *strings.Builder, data Data, width int) {
	var credits float64
	var calls int64
	for _, row := range data.Rows {
		credits += row.Totals.Credits
		calls += row.FunctionCalls
	}
	if data.Period == "day" {
		fmt.Fprintf(b, "Week %s daily credits: %s  Function calls: %s\n\n", formatDateLabel(data.PeriodStart), formatCredits(credits), formatInt(calls))
	} else {
		fmt.Fprintf(b, "Weekly credits: %s  Function calls: %s\n\n", formatCredits(credits), formatInt(calls))
	}
	writeCreditsGraph(b, data.Rows, width)
	b.WriteString("\n")
	tableRows := [][]string{{summaryFirstColumn(data), "Budget", "Text Tokens", "Uncached", "Cache Read", "Cache Hit", "FCalls"}}
	for i, row := range data.Rows {
		label := row.Label
		if i == data.SelectedIndex {
			label = selectedRowMarker + label
		}
		tableRows = append(tableRows, []string{
			label,
			progressBar(row.Totals.Credits, weeklyCreditBudget, 20),
			formatInt(row.Totals.TotalTokens),
			formatInt(row.Totals.UncachedInputTokens),
			formatInt(row.Totals.CacheReadInputTokens),
			fmt.Sprintf("%.1f%%", row.Totals.CacheHitRate*100),
			formatInt(row.FunctionCalls),
		})
	}
	columns := columnsForWidth(tableRows, width)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

func summaryFirstColumn(data Data) string {
	if data.Period == "day" {
		return "Day"
	}
	return "Week"
}

func writeCommandRows(b *strings.Builder, rows []Row, width int) {
	tableRows := [][]string{{"Command", "Kind", "FCalls", "Sessions", "Directories"}}
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			truncate(row.Label, 28),
			truncate(row.EventType, 14),
			formatInt(row.FunctionCalls),
			formatInt(row.SessionCount),
			row.Directory,
		})
	}
	columns := columnsForWidth(tableRows, width)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

func writePayloadSummary(b *strings.Builder, data Data, width int) {
	if data.Session == "" {
		var count, bytes int64
		for _, row := range data.Rows {
			count += row.Count
			bytes += row.PayloadBytes
		}
		fmt.Fprintf(b, "Payload groups: %d  Events: %s  Payload bytes: %s\n", len(data.Rows), formatInt(count), formatInt(bytes))
		return
	}
	fmt.Fprintf(b, "Session: %s\n", data.Session)
	if data.Interaction != "" {
		fmt.Fprintf(b, "Interaction: %s\n", compactTime(data.Interaction))
	}
	if len(data.Summary) == 0 {
		return
	}
	tableRows := [][]string{{"Metric", "Phase", "Count", "Payload Total", "Avg Bytes", "Max Bytes", "Avg Dur", "Max Dur", "Avg TTFT"}}
	for _, row := range data.Summary {
		tableRows = append(tableRows, []string{
			truncate(row.Label, 26),
			truncate(row.Phase, 12),
			formatInt(row.Count),
			formatInt(row.PayloadBytes),
			formatInt(row.AvgBytes),
			formatInt(row.MaxBytes),
			formatDuration(row.AvgDurationMS),
			formatDuration(row.MaxDurationMS),
			formatDuration(row.AvgTTFTMS),
		})
	}
	columns := columnsForWidth(tableRows, width)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

func writePayloadRows(b *strings.Builder, rows []Row, width int) {
	tableRows := [][]string{{"Payload", "Phase", "Count", "Payload Bytes", "Avg Bytes", "Max Bytes", "Avg Dur", "Max Dur", "Avg TTFT"}}
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			truncate(row.Label, 30),
			truncate(row.Phase, 12),
			formatInt(row.Count),
			formatInt(row.PayloadBytes),
			formatInt(row.AvgBytes),
			formatInt(row.MaxBytes),
			formatDuration(row.AvgDurationMS),
			formatDuration(row.MaxDurationMS),
			formatDuration(row.AvgTTFTMS),
		})
	}
	columns := columnsForWidth(tableRows, width)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

func writePayloadSessionRows(b *strings.Builder, rows []Row, selectedIndex int, width int) {
	tableRows := [][]string{{"Interaction", "Total", "Uncached", "Cache Read", "Output", "Reasoning", "Payload Bytes", "Dur", "TTFT", "Hit Rate"}}
	for i, row := range rows {
		label := compactTime(row.Label)
		if i == selectedIndex {
			label = selectedRowMarker + label
		}
		tableRows = append(tableRows, []string{
			label,
			formatInt(row.Totals.TotalTokens),
			formatInt(row.Totals.UncachedInputTokens),
			formatInt(row.Totals.CacheReadInputTokens),
			formatInt(row.Totals.OutputTokens),
			formatInt(row.Totals.ReasoningOutputTokens),
			formatInt(row.PayloadBytes),
			formatDuration(row.AvgDurationMS),
			formatDuration(row.AvgTTFTMS),
			fmt.Sprintf("%.1f%%", row.Totals.CacheHitRate*100),
		})
	}
	columns := columnsForWidth(tableRows, width)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

func writeTotals(b *strings.Builder, totals Totals) {
	fmt.Fprintf(b, "Credits: %s  Total: %s  Uncached: %s  Cache read: %s  Cache creation: %s  Output: %s  Reasoning: %s  Cache hit: %.1f%%\n",
		formatCredits(totals.Credits),
		formatInt(totals.TotalTokens),
		formatInt(totals.UncachedInputTokens),
		formatInt(totals.CacheReadInputTokens),
		formatInt(totals.CacheCreationInputTokens),
		formatInt(totals.OutputTokens),
		formatInt(totals.ReasoningOutputTokens),
		totals.CacheHitRate*100,
	)
}

func writeRows(b *strings.Builder, rows []Row, view string, selectedIndex int, width int) {
	includeDirectory := hasDirectory(rows)
	includeModel := view == "sessions" && hasModel(rows)
	includeCalls := view == "sessions" || hasFunctionCalls(rows)
	headers := []string{"Group"}
	if includeDirectory {
		headers = append(headers, "Directory")
	}
	if includeModel {
		headers = append(headers, "Model")
	}
	headers = append(headers, "Credits")
	if includeCalls {
		headers = append(headers, "FCalls")
	}
	headers = append(headers, "Total", "Uncached", "Cache Read", "Output", "Reasoning", "Hit Rate")
	tableRows := [][]string{headers}
	for i, row := range rows {
		label := row.Label
		if view == "sessions" && i == selectedIndex {
			label = selectedRowMarker + label
		}
		values := []string{truncate(label, 36)}
		if includeDirectory {
			values = append(values, truncate(shortDirectory(row.Directory), 32))
		}
		if includeModel {
			values = append(values, truncate(row.Model, 14))
		}
		values = append(values, formatCredits(row.Totals.Credits))
		if includeCalls {
			values = append(values, formatInt(row.FunctionCalls))
		}
		values = append(values,
			formatInt(row.Totals.TotalTokens),
			formatInt(row.Totals.UncachedInputTokens),
			formatInt(row.Totals.CacheReadInputTokens),
			formatInt(row.Totals.OutputTokens),
			formatInt(row.Totals.ReasoningOutputTokens),
			fmt.Sprintf("%.1f%%", row.Totals.CacheHitRate*100),
		)
		tableRows = append(tableRows, values)
	}
	columns := columnsForWidth(tableRows, width)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

type tableColumn struct {
	width int
	align alignment
}

type alignment int

const (
	alignLeft alignment = iota
	alignCenter
)

func columnsFor(rows [][]string) []tableColumn {
	if len(rows) == 0 {
		return nil
	}
	columns := make([]tableColumn, len(rows[0]))
	for i, header := range rows[0] {
		columns[i] = tableColumnFor(header)
	}
	for _, row := range rows {
		for i, value := range row {
			if i >= len(columns) {
				continue
			}
			if width := displayWidth(value); width > columns[i].width {
				columns[i].width = width
			}
		}
	}
	return columns
}

func columnsForWidth(rows [][]string, targetWidth int) []tableColumn {
	columns := columnsFor(rows)
	if targetWidth <= 0 || len(columns) == 0 {
		return columns
	}
	currentWidth := tableWidth(columns)
	if currentWidth >= targetWidth {
		return columns
	}
	extra := targetWidth - currentWidth
	base := extra / len(columns)
	remainder := extra % len(columns)
	for i := range columns {
		columns[i].width += base
		if i < remainder {
			columns[i].width++
		}
	}
	return columns
}

func tableWidth(columns []tableColumn) int {
	if len(columns) == 0 {
		return 0
	}
	width := len(columns) - 1
	for _, col := range columns {
		width += col.width
	}
	return width
}

func tableColumnFor(header string) tableColumn {
	switch header {
	case "Command":
		return tableColumn{width: 28, align: alignLeft}
	case "Payload":
		return tableColumn{width: 30, align: alignLeft}
	case "Metric":
		return tableColumn{width: 26, align: alignLeft}
	case "Interaction":
		return tableColumn{width: 16, align: alignCenter}
	case "Kind":
		return tableColumn{width: 14, align: alignCenter}
	case "Group":
		return tableColumn{width: 36, align: alignLeft}
	case "Week", "Day":
		return tableColumn{width: 9, align: alignLeft}
	case "Directory":
		return tableColumn{width: 32, align: alignLeft}
	case "Model":
		return tableColumn{width: 14, align: alignCenter}
	case "Directories":
		return tableColumn{width: 12, align: alignCenter}
	case "Sessions":
		return tableColumn{width: 10, align: alignCenter}
	case "Count", "FCalls":
		return tableColumn{width: 8, align: alignCenter}
	case "Credits":
		return tableColumn{width: 10, align: alignCenter}
	case "Budget":
		return tableColumn{width: 32, align: alignCenter}
	case "Payload Bytes":
		return tableColumn{width: 13, align: alignCenter}
	case "Payload Total", "Avg Bytes", "Max Bytes":
		return tableColumn{width: 10, align: alignCenter}
	case "Avg Dur", "Max Dur", "Avg TTFT", "Dur", "TTFT":
		return tableColumn{width: 9, align: alignCenter}
	case "Hit Rate":
		return tableColumn{width: 10, align: alignCenter}
	default:
		return tableColumn{width: 12, align: alignCenter}
	}
}

func writeTableLine(b *strings.Builder, columns []tableColumn, values []string) {
	for i, col := range columns {
		if i > 0 {
			b.WriteByte(' ')
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		switch col.align {
		case alignCenter:
			b.WriteString(center(value, col.width))
		default:
			b.WriteString(padRight(value, col.width))
		}
	}
	b.WriteByte('\n')
}

func center(value string, width int) string {
	valueWidth := displayWidth(value)
	if valueWidth >= width {
		return value
	}
	padding := width - valueWidth
	left := padding / 2
	right := padding - left
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
}

func padRight(value string, width int) string {
	valueWidth := displayWidth(value)
	if valueWidth >= width {
		return value
	}
	return value + strings.Repeat(" ", width-valueWidth)
}

func displayWidth(value string) int {
	return lipgloss.Width(value)
}

func writeGraph(b *strings.Builder, rows []Row, view string, width int) {
	values := make([]float64, 0, len(rows))
	for _, row := range rows {
		values = append(values, float64(row.Totals.TotalTokens))
	}
	if len(values) == 0 {
		return
	}
	options := []asciigraph.Option{
		asciigraph.Height(8),
		asciigraph.YAxisValueFormatter(graphValueFormatter(view)),
	}
	if width > 0 {
		options = append(options, asciigraph.Width(width))
	}
	b.WriteString(asciigraph.Plot(values, options...))
	b.WriteString("\n")
}

func writeCreditsGraph(b *strings.Builder, rows []Row, width int) {
	if len(rows) == 0 {
		return
	}
	values := make([]float64, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		values = append(values, rows[i].Totals.Credits)
	}
	options := []asciigraph.Option{
		asciigraph.Height(8),
		asciigraph.YAxisValueFormatter(func(value float64) string {
			return formatCredits(value)
		}),
	}
	if width > 0 {
		options = append(options, asciigraph.Width(width))
	}
	b.WriteString(asciigraph.Plot(values, options...))
	b.WriteString("\n")
}

func graphValueFormatter(view string) func(float64) string {
	return func(value float64) string {
		return formatCompactFloat(value)
	}
}

func sumRows(rows []Row) Totals {
	var totals Totals
	for _, row := range rows {
		totals = addTotals(totals, row.Totals)
	}
	return withDerived(totals)
}

const weeklyCreditBudget = 10_000

func progressBar(value, max float64, width int) string {
	if width <= 0 {
		return ""
	}
	bar := progress.New(
		progress.WithWidth(width),
		progress.WithoutPercentage(),
		progress.WithColorProfile(termenv.Ascii),
	)
	return fmt.Sprintf("%s %s/%s", bar.ViewAs(progressRatio(value, max)), formatCredits(value), formatCredits(max))
}

func progressRatio(value, max float64) float64 {
	if max <= 0 {
		max = 1
	}
	ratio := value / max
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return ratio
}

func addTotals(a, b Totals) Totals {
	return withDerived(Totals{
		InputTokens:              a.InputTokens + b.InputTokens,
		CachedInputTokens:        a.CachedInputTokens + b.CachedInputTokens,
		UncachedInputTokens:      a.UncachedInputTokens + b.UncachedInputTokens,
		CacheReadInputTokens:     a.CacheReadInputTokens + b.CacheReadInputTokens,
		CacheCreationInputTokens: a.CacheCreationInputTokens + b.CacheCreationInputTokens,
		OutputTokens:             a.OutputTokens + b.OutputTokens,
		ReasoningOutputTokens:    a.ReasoningOutputTokens + b.ReasoningOutputTokens,
		TotalTokens:              a.TotalTokens + b.TotalTokens,
		Credits:                  a.Credits + b.Credits,
	})
}

func cacheHitRate(t Totals) float64 {
	denominator := t.CacheReadInputTokens + t.CacheCreationInputTokens + t.UncachedInputTokens
	if denominator == 0 {
		return 0
	}
	return float64(t.CacheReadInputTokens) / float64(denominator)
}

func withDerived(t Totals) Totals {
	if t.UncachedInputTokens == 0 && t.InputTokens > 0 {
		t.UncachedInputTokens = t.InputTokens - t.CachedInputTokens
		if t.UncachedInputTokens < 0 {
			t.UncachedInputTokens = t.InputTokens
		}
	}
	if t.CacheReadInputTokens == 0 {
		t.CacheReadInputTokens = t.CachedInputTokens
	}
	t.CacheHitRate = cacheHitRate(t)
	return t
}

func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	return formatCompactFloat(float64(n))
}

func formatCredits(n float64) string {
	if n < 0 {
		return "-" + formatCredits(-n)
	}
	if n == 0 {
		return "0"
	}
	if n < 1 {
		return trimCompactFloat(n)
	}
	return formatCompactFloat(n)
}

func formatCompactFloat(n float64) string {
	if n < 0 {
		return "-" + formatCompactFloat(-n)
	}
	units := []struct {
		value  float64
		suffix string
	}{
		{value: 1_000_000_000_000, suffix: "T"},
		{value: 1_000_000_000, suffix: "B"},
		{value: 1_000_000, suffix: "M"},
		{value: 1_000, suffix: "K"},
	}
	for _, unit := range units {
		if n >= unit.value {
			return trimCompactFloat(n/unit.value) + unit.suffix
		}
	}
	if math.Mod(n, 1) == 0 {
		return fmt.Sprintf("%.0f", n)
	}
	return trimCompactFloat(n)
}

func trimCompactFloat(value float64) string {
	var rendered string
	switch {
	case value < 10:
		rendered = fmt.Sprintf("%.2f", value)
	case value < 100:
		rendered = fmt.Sprintf("%.1f", value)
	default:
		rendered = fmt.Sprintf("%.0f", math.Round(value))
	}
	rendered = strings.TrimRight(rendered, "0")
	return strings.TrimRight(rendered, ".")
}

func truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "."
}

func compactTime(value string) string {
	if len(value) >= len("2006-01-02T15:04") {
		return value[:len("2006-01-02T15:04")]
	}
	return value
}

func formatDuration(ms int64) string {
	if ms <= 0 {
		return "0ms"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	seconds := float64(ms) / 1000
	if seconds < 60 {
		return trimCompactFloat(seconds) + "s"
	}
	minutes := seconds / 60
	return trimCompactFloat(minutes) + "m"
}

func formatDateLabel(value string) string {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%04d %s %d%s", parsed.Year(), parsed.Month().String(), parsed.Day(), ordinalSuffix(parsed.Day()))
}

func ordinalSuffix(day int) string {
	if day%100 >= 11 && day%100 <= 13 {
		return "th"
	}
	switch day % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

func diffMillis(start, end string) int64 {
	if start == "" || end == "" {
		return 0
	}
	startTime, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return 0
	}
	endTime, err := time.Parse(time.RFC3339Nano, end)
	if err != nil {
		return 0
	}
	if endTime.Before(startTime) {
		return 0
	}
	return endTime.Sub(startTime).Milliseconds()
}

func shortDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	parent := filepath.Base(filepath.Dir(cleaned))
	base := filepath.Base(cleaned)
	if parent == "." || parent == string(filepath.Separator) || parent == "" {
		return base
	}
	return filepath.Join(parent, base)
}

func hasDirectory(rows []Row) bool {
	for _, row := range rows {
		if row.Directory != "" {
			return true
		}
	}
	return false
}

func hasModel(rows []Row) bool {
	for _, row := range rows {
		if row.Model != "" {
			return true
		}
	}
	return false
}

func hasFunctionCalls(rows []Row) bool {
	for _, row := range rows {
		if row.FunctionCalls > 0 {
			return true
		}
	}
	return false
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

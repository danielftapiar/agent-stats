package views

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/danieltapia/agent-stats/internal/store"
	"github.com/guptarohit/asciigraph"
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
	CacheHitRate             float64 `json:"cache_hit_rate"`
}

type Row struct {
	Label         string `json:"label"`
	Directory     string `json:"directory,omitempty"`
	EventType     string `json:"event_type,omitempty"`
	FunctionCalls int64  `json:"function_calls,omitempty"`
	SessionCount  int64  `json:"session_count,omitempty"`
	FirstSeen     string `json:"first_seen,omitempty"`
	LastSeen      string `json:"last_seen,omitempty"`
	Totals        Totals `json:"totals"`
}

type Data struct {
	View   string `json:"view"`
	Totals Totals `json:"totals"`
	Rows   []Row  `json:"rows"`
}

func Load(ctx context.Context, db *store.DB, view string, limit int, now time.Time) (Data, error) {
	var (
		rows []Row
		err  error
	)
	switch view {
	case "summary", "tokens":
		rows, err = queryGrouped(ctx, db, "token_type", "", 0)
		if err == nil {
			totals, totalsErr := queryTotals(ctx, db, "")
			if totalsErr != nil {
				return Data{}, totalsErr
			}
			return Data{View: view, Totals: totals, Rows: rows}, nil
		}
	case "today":
		start := now.Format("2006-01-02")
		rows, err = queryGrouped(ctx, db, "session_id", "timestamp >= ?", limitOrDefault(limit), start)
	case "daily":
		rows, err = queryGrouped(ctx, db, "substr(timestamp, 1, 10)", "", 0)
	case "sessions":
		rows, err = queryGrouped(ctx, db, "session_id", "", limitOrDefault(limit))
	case "hourly":
		rows, err = queryGrouped(ctx, db, "substr(timestamp, 12, 2) || ':00'", "", 0)
	case "cache":
		rows, err = queryGrouped(ctx, db, "substr(timestamp, 1, 10)", "", 0)
	case "reasoning":
		rows, err = queryReasoning(ctx, db, "substr(timestamp, 1, 10)")
	case "top":
		rows, err = queryGrouped(ctx, db, "session_id", "", limitOrDefault(limit))
	case "commands":
		rows, err = queryCommands(ctx, db, limitOrDefault(limit))
	default:
		rows, err = queryGrouped(ctx, db, "substr(timestamp, 1, 10)", "", 0)
	}
	if err != nil {
		return Data{}, err
	}
	data := Data{View: view, Rows: rows}
	for _, row := range rows {
		data.Totals = addTotals(data.Totals, row.Totals)
	}
	data.Totals.CacheHitRate = cacheHitRate(data.Totals)
	return data, nil
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
		ORDER BY COUNT(*) DESC, command_name ASC`
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
			{Label: "uncached input", Totals: withDerived(Totals{TotalTokens: totals.InputTokens, InputTokens: totals.InputTokens})},
			{Label: "cache read", Totals: withDerived(Totals{TotalTokens: totals.CachedInputTokens, CachedInputTokens: totals.CachedInputTokens})},
			{Label: "cache creation", Totals: withDerived(Totals{})},
			{Label: "output", Totals: Totals{TotalTokens: totals.OutputTokens, OutputTokens: totals.OutputTokens}},
			{Label: "reasoning output", Totals: Totals{TotalTokens: totals.ReasoningOutputTokens, ReasoningOutputTokens: totals.ReasoningOutputTokens}},
		}, nil
	}

	query := fmt.Sprintf(`SELECT %s AS label,
		SUM(input_tokens), SUM(cached_input_tokens), SUM(output_tokens),
		SUM(reasoning_output_tokens), SUM(total_tokens)
		FROM token_events`, groupExpr)
	if groupExpr == "session_id" {
		query = `WITH source_meta AS (
			SELECT session_id, MAX(session_dir) AS session_dir, SUM(function_call_count) AS function_call_count
			FROM source_files
			GROUP BY session_id
		)
		SELECT token_events.session_id AS label,
		SUM(input_tokens), SUM(cached_input_tokens), SUM(output_tokens),
		SUM(reasoning_output_tokens), SUM(total_tokens),
		COALESCE(source_meta.session_dir, ''), COALESCE(source_meta.function_call_count, 0)
		FROM token_events
		LEFT JOIN source_meta ON source_meta.session_id = token_events.session_id`
	}
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY label"
	if groupExpr == "session_id" {
		query += ", source_meta.session_dir, source_meta.function_call_count"
	}
	query += " ORDER BY SUM(total_tokens) DESC"
	if groupExpr == "substr(timestamp, 1, 10)" || strings.Contains(groupExpr, "12, 2") {
		query = strings.Replace(query, "ORDER BY SUM(total_tokens) DESC", "ORDER BY label ASC", 1)
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
				&row.Directory,
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
			); err != nil {
				return nil, err
			}
		}
		row.Totals = withDerived(row.Totals)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryReasoning(ctx context.Context, db *store.DB, groupExpr string) ([]Row, error) {
	rows, err := queryGrouped(ctx, db, groupExpr, "reasoning_output_tokens > 0", 0)
	if err != nil {
		return nil, err
	}
	if err := attachFunctionCallsByDate(ctx, db, rows); err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Label < rows[j].Label
	})
	return rows, nil
}

func attachFunctionCallsByDate(ctx context.Context, db *store.DB, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	byDate := make(map[string]int64, len(rows))
	sqlRows, err := db.Query(ctx, `SELECT substr(COALESCE(NULLIF(last_seen_at, ''), started_at), 1, 10) AS label,
		COALESCE(SUM(function_call_count), 0)
		FROM source_files
		WHERE COALESCE(NULLIF(last_seen_at, ''), started_at) != ''
		GROUP BY label`)
	if err != nil {
		return err
	}
	defer sqlRows.Close()
	for sqlRows.Next() {
		var label string
		var calls int64
		if err := sqlRows.Scan(&label, &calls); err != nil {
			return err
		}
		byDate[label] = calls
	}
	if err := sqlRows.Err(); err != nil {
		return err
	}
	for i := range rows {
		rows[i].FunctionCalls = byDate[rows[i].Label]
	}
	return nil
}

func queryTotals(ctx context.Context, db *store.DB, where string, args ...any) (Totals, error) {
	query := `SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_output_tokens), 0), COALESCE(SUM(total_tokens), 0) FROM token_events`
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
		if err := rows.Scan(&totals.InputTokens, &totals.CachedInputTokens, &totals.OutputTokens, &totals.ReasoningOutputTokens, &totals.TotalTokens); err != nil {
			return Totals{}, err
		}
	}
	return withDerived(totals), rows.Err()
}

func Render(data Data, view string) string {
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
		writeCommandRows(&b, data.Rows)
		return b.String()
	}
	writeTotals(&b, data.Totals)
	if len(data.Rows) == 0 {
		b.WriteString("\nNo token events found.\n")
		return b.String()
	}
	b.WriteString("\n")
	switch view {
	case "daily", "hourly", "cache", "reasoning":
		writeGraph(&b, data.Rows, view)
		b.WriteString("\n")
	}
	writeRows(&b, data.Rows, view)
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

func writeCommandRows(b *strings.Builder, rows []Row) {
	tableRows := [][]string{{"Command", "Kind", "Calls", "Sessions", "Directories", "First Seen", "Last Seen"}}
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			truncate(row.Label, 28),
			truncate(row.EventType, 14),
			formatInt(row.FunctionCalls),
			formatInt(row.SessionCount),
			row.Directory,
			compactTime(row.FirstSeen),
			compactTime(row.LastSeen),
		})
	}
	columns := columnsFor(tableRows)
	for _, row := range tableRows {
		writeTableLine(b, columns, row)
	}
}

func writeTotals(b *strings.Builder, totals Totals) {
	fmt.Fprintf(b, "Total: %s  Uncached: %s  Cache read: %s  Cache creation: %s  Output: %s  Reasoning: %s  Cache hit: %.1f%%\n",
		formatInt(totals.TotalTokens),
		formatInt(totals.UncachedInputTokens),
		formatInt(totals.CacheReadInputTokens),
		formatInt(totals.CacheCreationInputTokens),
		formatInt(totals.OutputTokens),
		formatInt(totals.ReasoningOutputTokens),
		totals.CacheHitRate*100,
	)
}

func writeRows(b *strings.Builder, rows []Row, view string) {
	includeDirectory := hasDirectory(rows)
	includeCalls := view == "sessions" || view == "reasoning" || hasFunctionCalls(rows)
	headers := []string{"Group"}
	if includeDirectory {
		headers = append(headers, "Directory")
	}
	if includeCalls {
		headers = append(headers, "Calls")
	}
	headers = append(headers, "Total", "Uncached", "Cache Read", "Output", "Reasoning", "Hit Rate")
	tableRows := [][]string{headers}
	for _, row := range rows {
		values := []string{truncate(row.Label, 36)}
		if includeDirectory {
			values = append(values, truncate(row.Directory, 32))
		}
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
	columns := columnsFor(tableRows)
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
			if len(value) > columns[i].width {
				columns[i].width = len(value)
			}
		}
	}
	return columns
}

func tableColumnFor(header string) tableColumn {
	switch header {
	case "Command":
		return tableColumn{width: 28, align: alignLeft}
	case "Kind":
		return tableColumn{width: 14, align: alignCenter}
	case "Group":
		return tableColumn{width: 36, align: alignLeft}
	case "Directory":
		return tableColumn{width: 32, align: alignLeft}
	case "Directories":
		return tableColumn{width: 12, align: alignCenter}
	case "Sessions":
		return tableColumn{width: 10, align: alignCenter}
	case "First Seen", "Last Seen":
		return tableColumn{width: 16, align: alignCenter}
	case "Calls":
		return tableColumn{width: 8, align: alignCenter}
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
	if len(value) >= width {
		return value
	}
	padding := width - len(value)
	left := padding / 2
	right := padding - left
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

func writeGraph(b *strings.Builder, rows []Row, view string) {
	values := make([]float64, 0, len(rows))
	for _, row := range rows {
		value := float64(row.Totals.TotalTokens)
		if view == "cache" {
			value = row.Totals.CacheHitRate * 100
		}
		if view == "reasoning" {
			value = float64(row.Totals.ReasoningOutputTokens)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return
	}
	b.WriteString(asciigraph.Plot(values, asciigraph.Height(8), asciigraph.YAxisValueFormatter(graphValueFormatter(view))))
	b.WriteString("\n")
}

func graphValueFormatter(view string) func(float64) string {
	return func(value float64) string {
		if view == "cache" {
			return fmt.Sprintf("%.1f%%", value)
		}
		return formatCompactFloat(value)
	}
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
	if t.UncachedInputTokens == 0 {
		t.UncachedInputTokens = t.InputTokens
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

func hasDirectory(rows []Row) bool {
	for _, row := range rows {
		if row.Directory != "" {
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

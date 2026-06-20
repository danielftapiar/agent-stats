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
	InputTokens           int64   `json:"input_tokens"`
	CachedInputTokens     int64   `json:"cached_input_tokens"`
	OutputTokens          int64   `json:"output_tokens"`
	ReasoningOutputTokens int64   `json:"reasoning_output_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	CacheHitRate          float64 `json:"cache_hit_rate"`
}

type Row struct {
	Label  string `json:"label"`
	Totals Totals `json:"totals"`
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

func queryGrouped(ctx context.Context, db *store.DB, groupExpr, where string, limit int, args ...any) ([]Row, error) {
	if groupExpr == "token_type" {
		totals, err := queryTotals(ctx, db, where, args...)
		if err != nil {
			return nil, err
		}
		return []Row{
			{Label: "input", Totals: Totals{TotalTokens: totals.InputTokens, InputTokens: totals.InputTokens}},
			{Label: "cached input", Totals: Totals{TotalTokens: totals.CachedInputTokens, CachedInputTokens: totals.CachedInputTokens}},
			{Label: "output", Totals: Totals{TotalTokens: totals.OutputTokens, OutputTokens: totals.OutputTokens}},
			{Label: "reasoning output", Totals: Totals{TotalTokens: totals.ReasoningOutputTokens, ReasoningOutputTokens: totals.ReasoningOutputTokens}},
		}, nil
	}

	query := fmt.Sprintf(`SELECT %s AS label,
		SUM(input_tokens), SUM(cached_input_tokens), SUM(output_tokens),
		SUM(reasoning_output_tokens), SUM(total_tokens)
		FROM token_events`, groupExpr)
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY label ORDER BY SUM(total_tokens) DESC"
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
		row.Totals.CacheHitRate = cacheHitRate(row.Totals)
		rows = append(rows, row)
	}
	return rows, sqlRows.Err()
}

func queryReasoning(ctx context.Context, db *store.DB, groupExpr string) ([]Row, error) {
	rows, err := queryGrouped(ctx, db, groupExpr, "reasoning_output_tokens > 0", 0)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Label < rows[j].Label
	})
	return rows, nil
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
	totals.CacheHitRate = cacheHitRate(totals)
	return totals, rows.Err()
}

func Render(data Data, view string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", strings.ToUpper(view))
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

func writeTotals(b *strings.Builder, totals Totals) {
	fmt.Fprintf(b, "Total: %s  Input: %s  Cached: %s  Output: %s  Reasoning: %s  Cache hit: %.1f%%\n",
		formatInt(totals.TotalTokens),
		formatInt(totals.InputTokens),
		formatInt(totals.CachedInputTokens),
		formatInt(totals.OutputTokens),
		formatInt(totals.ReasoningOutputTokens),
		totals.CacheHitRate*100,
	)
}

func writeRows(b *strings.Builder, rows []Row, view string) {
	tableRows := [][]string{{"Group", "Total", "Input", "Cached", "Output", "Reasoning", "Cache"}}
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			truncate(row.Label, 36),
			formatInt(row.Totals.TotalTokens),
			formatInt(row.Totals.InputTokens),
			formatInt(row.Totals.CachedInputTokens),
			formatInt(row.Totals.OutputTokens),
			formatInt(row.Totals.ReasoningOutputTokens),
			fmt.Sprintf("%.1f%%", row.Totals.CacheHitRate*100),
		})
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

var baseTableColumns = []tableColumn{
	{width: 36, align: alignLeft},
	{width: 12, align: alignCenter},
	{width: 12, align: alignCenter},
	{width: 12, align: alignCenter},
	{width: 12, align: alignCenter},
	{width: 12, align: alignCenter},
	{width: 10, align: alignCenter},
}

func columnsFor(rows [][]string) []tableColumn {
	columns := make([]tableColumn, len(baseTableColumns))
	copy(columns, baseTableColumns)
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
	b.WriteString(asciigraph.Plot(values, asciigraph.Height(8)))
	b.WriteString("\n")
}

func addTotals(a, b Totals) Totals {
	return Totals{
		InputTokens:           a.InputTokens + b.InputTokens,
		CachedInputTokens:     a.CachedInputTokens + b.CachedInputTokens,
		OutputTokens:          a.OutputTokens + b.OutputTokens,
		ReasoningOutputTokens: a.ReasoningOutputTokens + b.ReasoningOutputTokens,
		TotalTokens:           a.TotalTokens + b.TotalTokens,
	}
}

func cacheHitRate(t Totals) float64 {
	denominator := t.CachedInputTokens + t.InputTokens
	if denominator == 0 {
		return 0
	}
	return float64(t.CachedInputTokens) / float64(denominator)
}

func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
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
		if float64(n) >= unit.value {
			return trimCompactFloat(float64(n)/unit.value) + unit.suffix
		}
	}
	return fmt.Sprintf("%d", n)
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

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

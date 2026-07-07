package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/danieltapia/agent-stats/internal/store"
	"github.com/danieltapia/agent-stats/internal/views"
)

func TestSmallWindowUsesScrollableViewport(t *testing.T) {
	m := newTestModel(manyRowsData(40))
	m.active = viewIndex("today")
	m.configureViewport()
	m.setViewportContent()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 72, Height: 12})
	m = updated.(model)

	if m.viewport.Height < 1 {
		t.Fatalf("expected viewport height to stay usable, got %d", m.viewport.Height)
	}
	if visibleLineCount(m.View()) > 12 {
		t.Fatalf("expected rendered UI to fit within small window, got %d lines:\n%s", visibleLineCount(m.View()), m.View())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.viewport.YOffset == 0 {
		t.Fatal("expected down key to scroll viewport content")
	}
}

func TestThemeRendersVisibleFrameAndContentAccents(t *testing.T) {
	m := newTestModel(manyRowsData(2))
	m.width = 100
	m.height = 28
	m.configureViewport()
	m.setViewportContent()

	rendered := m.View()
	for _, want := range []string{"agent-stats", "SUMMARY", "Weekly credits:", "Week", "Budget"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected themed view to contain %q, got:\n%s", want, rendered)
		}
	}
}

func TestSelectedRowRendersWithoutVisibleMarker(t *testing.T) {
	m := newTestModel(views.Data{
		View:          "sessions",
		SelectedIndex: 0,
		Rows: []views.Row{{
			Label:  "session-a",
			Totals: views.Totals{InputTokens: 10, TotalTokens: 10},
		}},
	})
	m.active = viewIndex("sessions")
	m.row = 0

	rendered := m.renderContent()
	if strings.Contains(rendered, "> session-a") {
		t.Fatalf("expected selected row to omit visible marker:\n%s", rendered)
	}
	if !strings.Contains(rendered, "session-a") {
		t.Fatalf("expected selected row label to remain visible:\n%s", rendered)
	}
}

func TestProgressBarsUseThemeAccent(t *testing.T) {
	m := newTestModel(views.Data{})
	if m.theme.Progress.GetForeground() != m.theme.TableHeader.GetBackground() {
		t.Fatalf("expected progress foreground to match table header background")
	}
	rendered := m.themeProgressBars("████░░░░ 2K/10K")
	if !strings.Contains(rendered, "2K/10K") {
		t.Fatalf("expected progress label to remain unmodified, got %q", rendered)
	}
}

func TestVimKeysScrollHorizontally(t *testing.T) {
	m := newTestModel(views.Data{
		View: "sessions",
		Rows: []views.Row{{
			Label:     "session-with-a-long-visible-row",
			Directory: "/Users/example/some/really/long/project/path",
			Model:     "gpt-5.5-codex",
			Totals:    views.Totals{InputTokens: 1000, TotalTokens: 1000},
		}},
	})
	m.active = viewIndex("sessions")
	m.viewport = viewport.New(24, 8)
	m.viewport.SetHorizontalStep(8)
	m.setViewportContent()
	before := m.viewport.View()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	afterRight := m.viewport.View()
	if before == afterRight {
		t.Fatal("expected l to scroll the viewport horizontally")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(model)
	afterLeft := m.viewport.View()
	if afterLeft != before {
		t.Fatal("expected h to scroll the viewport back left")
	}
}

func TestSessionSelectionScrollsVerticallyAtBottom(t *testing.T) {
	data := manyRowsData(30)
	data.View = "sessions"
	m := newTestModel(data)
	m.active = viewIndex("sessions")
	m.viewport = viewport.New(80, 8)
	m.setViewportContent()

	for i := 0; i < 20; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = updated.(model)
	}

	if m.row != 20 {
		t.Fatalf("expected selected row 20, got %d", m.row)
	}
	if m.viewport.YOffset == 0 {
		t.Fatal("expected viewport to scroll down with selected session row")
	}
	if !strings.Contains(m.viewport.View(), "session-20") {
		t.Fatalf("expected selected session to be visible after scrolling:\n%s", m.viewport.View())
	}
}

func TestSummaryVimSelectionDrillsIntoSelectedWeek(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveFileSync(ctx, store.SourceFile{
		Path:            "source.jsonl",
		SizeBytes:       10,
		ModTimeUnix:     1,
		ProcessedOffset: 10,
		SessionID:       "session-a",
		Model:           "gpt-5.5",
	}, []store.TokenEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-05-18T10:00:00Z", InputTokens: 1_000_000, CachedInputTokens: 900_000, OutputTokens: 10_000, TotalTokens: 1_010_000, Model: "gpt-5.5"},
	}); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(views.Data{
		View: "summary",
		Rows: []views.Row{
			{Label: "2026 May 25th", PeriodStart: "2026-05-25"},
			{Label: "2026 May 18th", PeriodStart: "2026-05-18"},
		},
	})
	m.ctx = ctx
	m.db = db
	m.active = viewIndex("summary")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(model)
	if m.summaryRow != 1 {
		t.Fatalf("expected j to select second summary row, got %d", m.summaryRow)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.summaryWeek != "2026-05-18" {
		t.Fatalf("expected enter to drill into selected week, got %q", m.summaryWeek)
	}
	if m.data.Period != "day" {
		t.Fatalf("expected day drilldown data, got period %q", m.data.Period)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if viewNames[m.active] != "sessions" {
		t.Fatalf("expected selecting a day to switch to sessions, got %q", viewNames[m.active])
	}
	if m.sessionsDay != "2026-05-18" {
		t.Fatalf("expected sessions to be filtered by selected day, got %q", m.sessionsDay)
	}
	if m.data.Period != "day" || m.data.PeriodStart != "2026-05-18" {
		t.Fatalf("expected day-filtered sessions data, got period=%q start=%q", m.data.Period, m.data.PeriodStart)
	}
	if len(m.data.Rows) != 1 || m.data.Rows[0].Label != "session-a" {
		t.Fatalf("expected filtered session-a row, got %#v", m.data.Rows)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.sessionsDay != "" {
		t.Fatalf("expected escape to clear sessions day filter, got %q", m.sessionsDay)
	}
}

func TestSummaryUpAtFirstRowScrollsBackToGraph(t *testing.T) {
	m := newTestModel(manyRowsData(12))
	m.active = viewIndex("summary")
	m.summaryRow = 0
	m.viewport = viewport.New(80, 6)
	m.setViewportContent()
	before := m.viewport.YOffset
	if before == 0 {
		t.Fatal("expected selected first summary row to be below the graph in a small viewport")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)

	if m.summaryRow != 0 {
		t.Fatalf("expected first summary row to remain selected, got %d", m.summaryRow)
	}
	if m.viewport.YOffset >= before {
		t.Fatalf("expected up at first summary row to scroll toward graph, before=%d after=%d", before, m.viewport.YOffset)
	}
}

func TestTodayBlockSelectionDrillsIntoFilteredSessions(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveFileSyncWithDetails(ctx, store.SourceFile{
		Path:            "source.jsonl",
		SizeBytes:       10,
		ModTimeUnix:     1,
		ProcessedOffset: 10,
		SessionID:       "session-a",
		Model:           "gpt-5.5",
	}, []store.TokenEvent{
		{SessionID: "session-a", SourcePath: "source.jsonl", Timestamp: "2026-06-20T09:00:00Z", InputTokens: 10, TotalTokens: 10, Model: "gpt-5.5"},
		{SessionID: "session-b", SourcePath: "source.jsonl", Timestamp: "2026-06-20T14:00:00Z", InputTokens: 20, TotalTokens: 20, Model: "gpt-5.5"},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(views.Data{
		View: "today",
		GraphRows: []views.Row{
			{Label: "00:00-08:00", FirstSeen: "2026-06-20T00:00:00Z", LastSeen: "2026-06-20T08:00:00Z"},
			{Label: "08:00-18:00", FirstSeen: "2026-06-20T08:00:00Z", LastSeen: "2026-06-20T18:00:00Z"},
			{Label: "18:00-00:00", FirstSeen: "2026-06-20T18:00:00Z", LastSeen: "2026-06-21T00:00:00Z"},
		},
	})
	m.ctx = ctx
	m.db = db
	m.active = viewIndex("today")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.todayBlock != 1 {
		t.Fatalf("expected selected today block 1, got %d", m.todayBlock)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	if viewNames[m.active] != "sessions" {
		t.Fatalf("expected enter to switch to sessions, got %q", viewNames[m.active])
	}
	if m.sessionsLabel != "08:00-18:00" {
		t.Fatalf("expected selected time block label, got %q", m.sessionsLabel)
	}
	if m.data.Period != "time_block" || m.data.PeriodStart != "08:00-18:00" {
		t.Fatalf("expected time-block filtered sessions data, got period=%q start=%q", m.data.Period, m.data.PeriodStart)
	}
	if len(m.data.Rows) != 2 {
		t.Fatalf("expected both sessions in 08:00-18:00 block, got %#v", m.data.Rows)
	}
}

func TestSelectedRowCanScrollHorizontallyInSmallWindow(t *testing.T) {
	m := newTestModel(views.Data{
		View:          "sessions",
		SelectedIndex: 0,
		Rows: []views.Row{{
			Label:     "session-with-a-long-visible-row",
			Directory: "/Users/example/some/really/long/project/path",
			Model:     "gpt-5.5-codex",
			Totals:    views.Totals{InputTokens: 1000, TotalTokens: 1000},
		}},
	})
	m.active = viewIndex("sessions")
	m.width = 36
	m.height = 12
	m.configureViewport()
	m.setViewportContent()
	before := m.viewport.View()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	afterRight := m.viewport.View()
	if before == afterRight {
		t.Fatal("expected selected row to preserve horizontal overflow for scrolling")
	}
	if m.selectedRowWidth("session-with-a-long-visible-row") <= m.contentWidth() {
		t.Fatal("expected selected row style width to preserve the full row width")
	}
}

func TestDeleteSessionRequiresTypingYes(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionPath := filepath.Join(t.TempDir(), "rollout-session-a.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveFileSync(ctx, store.SourceFile{
		Path:            sessionPath,
		SizeBytes:       3,
		ModTimeUnix:     1,
		ProcessedOffset: 3,
		SessionID:       "session-a",
	}, []store.TokenEvent{{SessionID: "session-a", SourcePath: sessionPath, Timestamp: "2026-06-20T10:00:00Z", InputTokens: 1, TotalTokens: 1}}); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(views.Data{View: "sessions", Rows: []views.Row{{Label: "session-a"}}})
	m.ctx = ctx
	m.db = db
	m.active = viewIndex("sessions")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	if !m.confirmDelete {
		t.Fatal("expected d to ask for delete confirmation")
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("expected session file to remain before confirmation: %v", err)
	}

	m.input.SetValue("no")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.confirmDelete {
		t.Fatal("expected non-yes confirmation to close prompt")
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("expected session file to remain after non-yes confirmation: %v", err)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	m.input.SetValue("yes")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("expected session file to be deleted after typing yes, got err=%v", err)
	}
}

func TestThemeCommandOpensPickerAndAppliesPreview(t *testing.T) {
	m := newTestModel(manyRowsData(2))
	m.prompt = true
	m.input.Focus()
	m.input.SetValue("theme")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !m.picker {
		t.Fatal("expected :theme command to open theme picker")
	}
	if m.preview != m.selected {
		t.Fatalf("expected picker preview to start on selected theme, got preview=%d selected=%d", m.preview, m.selected)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.preview == m.selected {
		t.Fatal("expected down key to preview a different theme")
	}
	previewed := m.preview

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.picker {
		t.Fatal("expected enter to close theme picker")
	}
	if m.selected != previewed {
		t.Fatalf("expected enter to apply previewed theme %d, got %d", previewed, m.selected)
	}
}

func TestThemePickerCancelRestoresSelectedTheme(t *testing.T) {
	m := newTestModel(manyRowsData(2))
	m.picker = true
	m.preview = m.selected

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.preview == m.selected {
		t.Fatal("expected theme preview to change")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.picker {
		t.Fatal("expected escape to close theme picker")
	}
	if m.preview != m.selected {
		t.Fatalf("expected escape to restore selected theme, got preview=%d selected=%d", m.preview, m.selected)
	}
}

func newTestModel(data views.Data) model {
	themes := themeOptions()
	input := textinput.New()
	input.Prompt = ":"
	input.PromptStyle = themes[0].Theme.Prompt
	input.TextStyle = themes[0].Theme.Prompt
	return model{
		active:   0,
		input:    input,
		theme:    themes[0].Theme,
		themes:   themes,
		viewport: viewport.New(80, 20),
		data:     data,
	}
}

func manyRowsData(count int) views.Data {
	rows := make([]views.Row, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, views.Row{
			Label: fmt.Sprintf("session-%02d", i),
			Totals: views.Totals{
				InputTokens:       int64(1000 + i),
				CachedInputTokens: int64(2000 + i),
				OutputTokens:      int64(100 + i),
				TotalTokens:       int64(1100 + i),
				CacheHitRate:      0.5,
			},
		})
	}
	return views.Data{
		View: "summary",
		Totals: views.Totals{
			InputTokens:       1000,
			CachedInputTokens: 2000,
			OutputTokens:      100,
			TotalTokens:       1100,
			CacheHitRate:      0.5,
		},
		Rows: rows,
	}
}

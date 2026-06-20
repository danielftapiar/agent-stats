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
	m.active = viewIndex("daily")
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

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.summaryWeek != "" {
		t.Fatalf("expected escape to return to weekly summary, got %q", m.summaryWeek)
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

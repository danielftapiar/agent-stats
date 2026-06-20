package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/danieltapia/agent-stats/internal/views"
)

func TestSmallWindowUsesScrollableViewport(t *testing.T) {
	m := newTestModel(manyRowsData(40))
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
	for _, want := range []string{"agent-stats", "SUMMARY", "Total:", "Group", "┃"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected themed view to contain %q, got:\n%s", want, rendered)
		}
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

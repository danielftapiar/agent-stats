package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/danieltapia/agent-stats/internal/codex"
	"github.com/danieltapia/agent-stats/internal/store"
	"github.com/danieltapia/agent-stats/internal/views"
)

type tickMsg time.Time

var viewNames = []string{
	"summary",
	"today",
	"daily",
	"sessions",
	"cache",
	"reasoning",
	"commands",
	"payload",
	"tokens",
	"top",
}

type model struct {
	ctx      context.Context
	db       *store.DB
	indexer  *codex.Indexer
	active   int
	input    textinput.Model
	viewport viewport.Model
	prompt   bool
	picker   bool
	err      string
	data     views.Data
	width    int
	height   int
	lastSync time.Time
	theme    theme
	themes   []namedTheme
	selected int
	preview  int
	row      int
	session  string
}

func Run(ctx context.Context, db *store.DB, indexer *codex.Indexer) error {
	m := newModel(ctx, db, indexer)
	program := tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func newModel(ctx context.Context, db *store.DB, indexer *codex.Indexer) model {
	input := textinput.New()
	input.Prompt = ":"
	input.Placeholder = "summary"
	input.CharLimit = 32
	themes := themeOptions()
	t := themes[0].Theme
	input.PromptStyle = t.Prompt
	input.TextStyle = t.Prompt
	m := model{
		ctx:      ctx,
		db:       db,
		indexer:  indexer,
		input:    input,
		viewport: viewport.New(80, 20),
		theme:    t,
		themes:   themes,
	}
	m.reload()
	return m
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.configureViewport()
		m.setViewportContent()
		return m, nil
	case tickMsg:
		if err := m.indexer.SyncActive(m.ctx, 10*time.Minute); err != nil {
			m.err = err.Error()
		} else {
			m.err = ""
			m.lastSync = time.Time(msg)
			m.reload()
		}
		return m, tick()
	case tea.KeyMsg:
		if m.picker {
			return m.updateThemePicker(msg)
		}
		if m.prompt {
			return m.updatePrompt(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case ":":
			m.prompt = true
			m.input.Focus()
			m.input.SetValue("")
			return m, nil
		case "?":
			m.err = commandHelp()
			return m, nil
		case "enter":
			if viewNames[m.active] == "sessions" && len(m.data.Rows) > 0 {
				if m.row < 0 {
					m.row = 0
				}
				if m.row >= len(m.data.Rows) {
					m.row = len(m.data.Rows) - 1
				}
				m.session = m.data.Rows[m.row].Label
				if strings.HasPrefix(m.session, "> ") {
					m.session = strings.TrimPrefix(m.session, "> ")
				}
				if idx := viewIndex("payload"); idx >= 0 {
					m.active = idx
				}
				m.reload()
				return m, nil
			}
		case "j", "down":
			if viewNames[m.active] == "sessions" && len(m.data.Rows) > 0 {
				m.row++
				if m.row >= len(m.data.Rows) {
					m.row = len(m.data.Rows) - 1
				}
				m.setViewportContent()
				return m, nil
			}
		case "k", "up":
			if viewNames[m.active] == "sessions" && len(m.data.Rows) > 0 {
				m.row--
				if m.row < 0 {
					m.row = 0
				}
				m.setViewportContent()
				return m, nil
			}
		case "tab":
			m.active = (m.active + 1) % len(viewNames)
			m.session = ""
			m.reload()
			return m, nil
		case "shift+tab":
			m.active--
			if m.active < 0 {
				m.active = len(viewNames) - 1
			}
			m.session = ""
			m.reload()
			return m, nil
		}
		if len(msg.String()) == 1 {
			key := msg.String()[0]
			if key >= '1' && key <= '9' {
				next := int(key - '1')
				if next < len(viewNames) {
					m.active = next
					m.session = ""
					m.reload()
				}
			}
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.prompt = false
		m.input.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		m.prompt = false
		m.input.Blur()
		if value == "" {
			return m, nil
		}
		if value == "quit" || value == "q" {
			return m, tea.Quit
		}
		if value == "theme" {
			m.picker = true
			m.preview = m.selected
			m.err = ""
			return m, nil
		}
		if idx := viewIndex(value); idx >= 0 {
			m.active = idx
			if value != "payload" {
				m.session = ""
			}
			m.err = ""
			m.reload()
		} else if value == "help" {
			m.err = commandHelp()
		} else {
			m.err = fmt.Sprintf("unknown command :%s", value)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func commandHelp() string {
	return "commands: :summary :today :daily :sessions :cache :reasoning :commands :payload :tokens :top :theme :quit"
}

func (m model) updateThemePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.preview = m.selected
		m.theme = m.themes[m.selected].Theme
		m.applyThemeToInput()
		m.picker = false
		m.setViewportContent()
		return m, nil
	case "enter":
		m.selected = m.preview
		m.theme = m.themes[m.selected].Theme
		m.applyThemeToInput()
		m.picker = false
		m.setViewportContent()
		return m, nil
	case "up", "k", "shift+tab":
		m.preview--
		if m.preview < 0 {
			m.preview = len(m.themes) - 1
		}
	case "down", "j", "tab":
		m.preview = (m.preview + 1) % len(m.themes)
	}
	m.theme = m.themes[m.preview].Theme
	m.applyThemeToInput()
	m.setViewportContent()
	return m, nil
}

func (m *model) reload() {
	var (
		data views.Data
		err  error
	)
	if viewNames[m.active] == "payload" && m.session != "" {
		data, err = views.LoadSessionPayload(m.ctx, m.db, m.session, 20)
	} else {
		data, err = views.Load(m.ctx, m.db, viewNames[m.active], 20, time.Now())
	}
	if err != nil {
		m.err = err.Error()
		return
	}
	if viewNames[m.active] == "sessions" {
		if m.row >= len(data.Rows) {
			m.row = len(data.Rows) - 1
		}
		if m.row < 0 {
			m.row = 0
		}
		data.SelectedIndex = m.row
	}
	m.data = data
	m.setViewportContent()
}

func (m *model) configureViewport() {
	contentWidth := m.width - frameHorizontalSize
	if contentWidth < 20 {
		contentWidth = 20
	}
	contentHeight := m.height - frameVerticalSize - fixedChromeHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	m.viewport.Width = contentWidth
	m.viewport.Height = contentHeight
}

func (m *model) setViewportContent() {
	m.viewport.SetContent(m.renderContent())
}

func (m *model) applyThemeToInput() {
	m.input.PromptStyle = m.theme.Prompt
	m.input.TextStyle = m.theme.Prompt
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("agent-stats"))
	b.WriteString("\n")
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n\n")
	if !m.lastSync.IsZero() {
		b.WriteString(m.theme.Status.Render(fmt.Sprintf("Last sync: %s", m.lastSync.Format("15:04:05"))))
		b.WriteString("\n")
	}
	if m.err != "" {
		b.WriteString(m.theme.Error.Render(m.err))
		b.WriteString("\n")
	}
	if m.picker {
		b.WriteString(m.renderThemePicker())
	} else if m.prompt {
		b.WriteString(m.input.View())
	} else {
		b.WriteString(m.theme.Help.Render("Press : for commands, :theme for themes, arrows/PageUp/PageDown to scroll, q to quit"))
	}
	return m.theme.Frame.Render(b.String())
}

func (m model) renderContent() string {
	if viewNames[m.active] == "sessions" {
		m.data.SelectedIndex = m.row
	}
	return m.themeContent(views.Render(m.data, viewNames[m.active]))
}

func (m model) themeContent(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		switch {
		case line == "":
			continue
		case line == strings.ToUpper(viewNames[m.active]):
			lines[i] = m.theme.ViewTitle.Render(line)
		case strings.HasPrefix(line, "Total:"):
			lines[i] = m.theme.Totals.Render(line)
		case strings.HasPrefix(line, "Group"):
			lines[i] = m.theme.TableHeader.Render(line)
		case strings.ContainsAny(line, "┤┼╭╮╯╰─│"):
			lines[i] = m.theme.Graph.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderThemePicker() string {
	var b strings.Builder
	b.WriteString(m.theme.Help.Render("Theme: use up/down to preview, Enter to apply, Esc to cancel"))
	b.WriteString("\n")
	for i, option := range m.themes {
		prefix := "  "
		if i == m.preview {
			prefix = "> "
		}
		name := prefix + option.Name
		if i == m.selected {
			name += " *"
		}
		if i == m.preview {
			b.WriteString(m.theme.ActiveTab.Render(name))
		} else {
			b.WriteString(m.theme.Tab.Render(name))
		}
		if i < len(m.themes)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m model) renderTabs() string {
	var parts []string
	for i, name := range viewNames {
		label := fmt.Sprintf("%d %s", i+1, strings.Title(name))
		if i == m.active {
			parts = append(parts, m.theme.ActiveTab.Render(label))
		} else {
			parts = append(parts, m.theme.Tab.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

const (
	frameHorizontalSize = 6
	frameVerticalSize   = 4
	fixedChromeHeight   = 6
)

func visibleLineCount(rendered string) int {
	return lipgloss.Height(rendered)
}

func tick() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func viewIndex(name string) int {
	name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), ":")
	for i, view := range viewNames {
		if name == view {
			return i
		}
	}
	return -1
}

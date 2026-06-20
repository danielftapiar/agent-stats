package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	"hourly",
	"cache",
	"reasoning",
	"tokens",
	"top",
}

type model struct {
	ctx      context.Context
	db       *store.DB
	indexer  *codex.Indexer
	active   int
	input    textinput.Model
	prompt   bool
	err      string
	data     views.Data
	width    int
	height   int
	lastSync time.Time
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
	m := model{
		ctx:     ctx,
		db:      db,
		indexer: indexer,
		input:   input,
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
			m.err = "commands: :summary :today :daily :sessions :hourly :cache :reasoning :tokens :top :quit"
			return m, nil
		case "tab":
			m.active = (m.active + 1) % len(viewNames)
			m.reload()
			return m, nil
		case "shift+tab":
			m.active--
			if m.active < 0 {
				m.active = len(viewNames) - 1
			}
			m.reload()
			return m, nil
		}
		if len(msg.String()) == 1 {
			key := msg.String()[0]
			if key >= '1' && key <= '9' {
				next := int(key - '1')
				if next < len(viewNames) {
					m.active = next
					m.reload()
				}
			}
		}
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
		if idx := viewIndex(value); idx >= 0 {
			m.active = idx
			m.err = ""
			m.reload()
		} else if value == "help" {
			m.err = "commands: :summary :today :daily :sessions :hourly :cache :reasoning :tokens :top :quit"
		} else {
			m.err = fmt.Sprintf("unknown command :%s", value)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) reload() {
	data, err := views.Load(m.ctx, m.db, viewNames[m.active], 20, time.Now())
	if err != nil {
		m.err = err.Error()
		return
	}
	m.data = data
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString(views.Render(m.data, viewNames[m.active]))
	b.WriteString("\n")
	if !m.lastSync.IsZero() {
		fmt.Fprintf(&b, "Last sync: %s\n", m.lastSync.Format("15:04:05"))
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render(m.err))
		b.WriteString("\n")
	}
	if m.prompt {
		b.WriteString(m.input.View())
	} else {
		b.WriteString(helpStyle.Render("Press : for commands, 1-9 for tabs, Tab/Shift+Tab to switch, q to quit"))
	}
	return b.String()
}

func (m model) renderTabs() string {
	var parts []string
	for i, name := range viewNames {
		label := fmt.Sprintf("%d %s", i+1, strings.Title(name))
		if i == m.active {
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			parts = append(parts, tabStyle.Render(label))
		}
	}
	return strings.Join(parts, " ")
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

var (
	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("244"))
	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("30")).
			Bold(true)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
)

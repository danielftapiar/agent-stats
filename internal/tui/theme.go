package tui

import "github.com/charmbracelet/lipgloss"

type theme struct {
	Frame     lipgloss.Style
	Title     lipgloss.Style
	Tab       lipgloss.Style
	ActiveTab lipgloss.Style
	Status    lipgloss.Style
	Help      lipgloss.Style
	Error     lipgloss.Style
	Prompt    lipgloss.Style
}

func defaultTheme() theme {
	return theme{
		Frame: lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("#4B9C8E")),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F4D35E")),
		Tab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#8A95A3")),
		ActiveTab: lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#F4D35E")),
		Status: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B9C8E")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8A95A3")),
		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")),
		Prompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4D35E")),
	}
}

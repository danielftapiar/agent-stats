package tui

import "github.com/charmbracelet/lipgloss"

type namedTheme struct {
	Name  string
	Theme theme
}

type theme struct {
	Frame       lipgloss.Style
	Title       lipgloss.Style
	Tab         lipgloss.Style
	ActiveTab   lipgloss.Style
	Status      lipgloss.Style
	Help        lipgloss.Style
	Error       lipgloss.Style
	Prompt      lipgloss.Style
	ViewTitle   lipgloss.Style
	Totals      lipgloss.Style
	TableHeader lipgloss.Style
	Graph       lipgloss.Style
	Progress    lipgloss.Style
	SelectedRow lipgloss.Style
}

func defaultTheme() theme {
	return themeOptions()[0].Theme
}

func themeOptions() []namedTheme {
	return []namedTheme{
		{Name: "Signal", Theme: signalTheme()},
		{Name: "Graphite", Theme: graphiteTheme()},
		{Name: "High Contrast", Theme: highContrastTheme()},
	}
}

func signalTheme() theme {
	return theme{
		Frame: lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#4B9C8E")),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#F4D35E")).
			Padding(0, 1),
		Tab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#CAD3DF")).
			Background(lipgloss.Color("#24313A")),
		ActiveTab: lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#F4D35E")),
		Status: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4B9C8E")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8A95A3")),
		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")),
		Prompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4D35E")),
		ViewTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#4B9C8E")).
			Padding(0, 1),
		Totals: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F4D35E")),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#7BDFF2")),
		Graph: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7BDFF2")),
		Progress: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7BDFF2")),
		SelectedRow: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#F4D35E")),
	}
}

func graphiteTheme() theme {
	return theme{
		Frame: lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#A7B0BA")),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#DCE3EA")).
			Padding(0, 1),
		Tab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#CED6DE")).
			Background(lipgloss.Color("#30363D")),
		ActiveTab: lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#DCE3EA")),
		Status: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9ED0FF")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A7B0BA")),
		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF7A90")),
		Prompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DCE3EA")),
		ViewTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#9ED0FF")).
			Padding(0, 1),
		Totals: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#DCE3EA")),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#A7B0BA")),
		Graph: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ED0FF")),
		Progress: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A7B0BA")),
		SelectedRow: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#101418")).
			Background(lipgloss.Color("#DCE3EA")),
	}
}

func highContrastTheme() theme {
	return theme{
		Frame: lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#FFFFFF")),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFFFFF")).
			Padding(0, 1),
		Tab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#333333")),
		ActiveTab: lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFFF00")),
		Status: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFFF")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")),
		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF0000")),
		Prompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFF00")),
		ViewTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#00FFFF")).
			Padding(0, 1),
		Totals: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFF00")),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFFFFF")),
		Graph: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FFFF")),
		Progress: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")),
		SelectedRow: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFFF00")),
	}
}

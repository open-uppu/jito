package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme is a color/style preset for the TUI.
type Theme struct {
	Name        string
	Primary     lipgloss.Color
	Secondary   lipgloss.Color
	Success     lipgloss.Color
	Error       lipgloss.Color
	Muted       lipgloss.Color
	UserColor   lipgloss.Color
	AgentColor  lipgloss.Color
}

// Themes is the registry of available themes.
var Themes = map[string]Theme{
	"default": {
		Name:       "default",
		Primary:    lipgloss.Color("#7D56F4"),
		Secondary:  lipgloss.Color("#5B5BFF"),
		Success:    lipgloss.Color("#04B575"),
		Error:      lipgloss.Color("#FF5F87"),
		Muted:      lipgloss.Color("#888888"),
		UserColor:  lipgloss.Color("#04B575"),
		AgentColor: lipgloss.Color("#7D56F4"),
	},
	"dark": {
		Name:       "dark",
		Primary:    lipgloss.Color("#BB86FC"),
		Secondary:  lipgloss.Color("#03DAC6"),
		Success:    lipgloss.Color("#00C853"),
		Error:      lipgloss.Color("#CF6679"),
		Muted:      lipgloss.Color("#666666"),
		UserColor:  lipgloss.Color("#00E676"),
		AgentColor: lipgloss.Color("#BB86FC"),
	},
	"light": {
		Name:       "light",
		Primary:    lipgloss.Color("#5E35B1"),
		Secondary:  lipgloss.Color("#1E88E5"),
		Success:    lipgloss.Color("#2E7D32"),
		Error:      lipgloss.Color("#C62828"),
		Muted:      lipgloss.Color("#9E9E9E"),
		UserColor:  lipgloss.Color("#2E7D32"),
		AgentColor: lipgloss.Color("#5E35B1"),
	},
	"solarized": {
		Name:       "solarized",
		Primary:    lipgloss.Color("#268BD2"),
		Secondary:  lipgloss.Color("#2AA198"),
		Success:    lipgloss.Color("#859900"),
		Error:      lipgloss.Color("#DC322F"),
		Muted:      lipgloss.Color("#93A1A1"),
		UserColor:  lipgloss.Color("#859900"),
		AgentColor: lipgloss.Color("#268BD2"),
	},
	"nord": {
		Name:       "nord",
		Primary:    lipgloss.Color("#88C0D0"),
		Secondary:  lipgloss.Color("#81A1C1"),
		Success:    lipgloss.Color("#A3BE8C"),
		Error:      lipgloss.Color("#BF616A"),
		Muted:      lipgloss.Color("#4C566A"),
		UserColor:  lipgloss.Color("#A3BE8C"),
		AgentColor: lipgloss.Color("#88C0D0"),
	},
}

// GetTheme returns a theme by name (default if not found).
func GetTheme(name string) Theme {
	if t, ok := Themes[name]; ok {
		return t
	}
	return Themes["default"]
}

// StyleSet returns lipgloss styles built from a theme.
type StyleSet struct {
	Title   lipgloss.Style
	User    lipgloss.Style
	Agent   lipgloss.Style
	Error   lipgloss.Style
	Status  lipgloss.Style
	Success lipgloss.Style
}

// NewStyleSet builds lipgloss styles from a theme.
func NewStyleSet(t Theme) StyleSet {
	return StyleSet{
		Title: lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true).
			Padding(0, 1),
		User: lipgloss.NewStyle().
			Foreground(t.UserColor).
			Bold(true),
		Agent: lipgloss.NewStyle().
			Foreground(t.AgentColor).
			Bold(true),
		Error: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),
		Status: lipgloss.NewStyle().
			Foreground(t.Muted).
			Italic(true),
		Success: lipgloss.NewStyle().
			Foreground(t.Success).
			Bold(true),
	}
}
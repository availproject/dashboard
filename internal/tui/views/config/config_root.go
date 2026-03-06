package config

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/views"
)

var (
	cfgSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	cfgDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

var configMenuItems = []string{
	"Teams & Members",
	"Sources",
	"Business Metrics",
	"Annotations",
	"Users",
}

// ConfigRootView is the top-level config sub-menu.
type ConfigRootView struct {
	c      *client.Client
	cursor int
}

// NewConfigRootView creates a ConfigRootView.
func NewConfigRootView(c *client.Client) *ConfigRootView {
	return &ConfigRootView{c: c}
}

// Init implements tea.Model.
func (v *ConfigRootView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *ConfigRootView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.cursor < len(configMenuItems)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "enter":
			return v, v.pushSubView()
		}
	}
	return v, nil
}

func (v *ConfigRootView) pushSubView() tea.Cmd {
	var subView tea.Model
	switch v.cursor {
	case 0:
		subView = NewConfigTeamsView(v.c)
	case 1:
		subView = NewConfigSourcesView(v.c)
	case 3:
		subView = NewConfigAnnotationsView(v.c)
	case 4:
		subView = NewConfigUsersView(v.c)
	default:
		return nil
	}
	return func() tea.Msg { return views.PushViewMsg{View: subView} }
}

// View implements tea.Model.
func (v *ConfigRootView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config") + "\n\n")

	for i, item := range configMenuItems {
		prefix := "  "
		label := item
		if i == v.cursor {
			prefix = "> "
			label = cfgSelectedStyle.Render(item)
		}
		sb.WriteString(fmt.Sprintf("%s%s\n", prefix, label))
	}

	sb.WriteString("\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter to select  ·  Esc to go back") + "\n")
	return sb.String()
}

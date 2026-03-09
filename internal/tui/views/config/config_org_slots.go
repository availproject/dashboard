package config

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ConfigOrgSlotsView is a placeholder for the org-level slot configuration.
// Full implementation is a TODO — for now it shows a "coming soon" message.
type ConfigOrgSlotsView struct {
	c *client.Client
}

// NewConfigOrgSlotsView creates a ConfigOrgSlotsView.
func NewConfigOrgSlotsView(c *client.Client) *ConfigOrgSlotsView {
	return &ConfigOrgSlotsView{c: c}
}

// Init implements tea.Model.
func (v *ConfigOrgSlotsView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *ConfigOrgSlotsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
	}
	return v, nil
}

// View implements tea.Model.
func (v *ConfigOrgSlotsView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — Org") + "\n\n")
	sb.WriteString("  Org-level slot configuration coming soon.\n\n")
	sb.WriteString(cfgDimStyle.Render("  Esc back") + "\n")
	return sb.String()
}

// ConfigTeamListView lists teams and navigates to ConfigTeamSlotsView on Enter.
type ConfigTeamListView struct {
	c       *client.Client
	teams   []client.TeamItem
	loading bool
	errMsg  string
	cursor  int
}

type teamListLoadedMsg struct {
	teams []client.TeamItem
	err   error
}

// NewConfigTeamListView creates a ConfigTeamListView.
func NewConfigTeamListView(c *client.Client) *ConfigTeamListView {
	return &ConfigTeamListView{c: c, loading: true}
}

// Init implements tea.Model.
func (v *ConfigTeamListView) Init() tea.Cmd {
	c := v.c
	return func() tea.Msg {
		teams, err := c.GetTeams()
		return teamListLoadedMsg{teams: teams, err: err}
	}
}

// Update implements tea.Model.
func (v *ConfigTeamListView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case teamListLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.teams = m.teams
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.cursor < len(v.teams)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "enter":
			if !v.loading && v.cursor < len(v.teams) {
				team := v.teams[v.cursor]
				sv := NewConfigTeamSlotsView(v.c, team.ID, team.Name)
				return v, func() tea.Msg { return msgs.PushViewMsg{View: sv} }
			}
			return v, nil
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
	}
	return v, nil
}

// View implements tea.Model.
func (v *ConfigTeamListView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — Select Team") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}
	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString("\n" + cfgDimStyle.Render("  Esc back") + "\n")
		return sb.String()
	}

	if len(v.teams) == 0 {
		sb.WriteString("  No teams configured yet.\n")
	} else {
		for i, team := range v.teams {
			prefix := "  "
			label := team.Name
			if i == v.cursor {
				prefix = "> "
				label = cfgSelectedStyle.Render(team.Name)
			}
			sb.WriteString(prefix + label + "\n")
		}
	}

	sb.WriteString("\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter select  ·  Esc back") + "\n")
	return sb.String()
}

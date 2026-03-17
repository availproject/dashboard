package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- internal message types ----

type teamsLoadedMsg struct {
	teams []client.TeamItem
	err   error
}

type teamsReloadMsg struct{}

type teamsMutatedMsg struct{ err error }

// ---- mode enum ----

type configTeamsMode int

const (
	cfgTeamsModeNormal configTeamsMode = iota
	cfgTeamsModeInputNewTeam
	cfgTeamsModeInputEditTeam
	cfgTeamsModeInputNewMember
	cfgTeamsModeInputEditMember
	cfgTeamsModeConfirmDeleteTeam
	cfgTeamsModeConfirmDeleteMember
	cfgTeamsModeInputMarketingLabel
)

// ConfigTeamsView manages teams and members.
type ConfigTeamsView struct {
	c            *client.Client
	teams        []client.TeamItem
	loading      bool
	errMsg       string
	teamCursor   int
	memberCursor int
	expanded     bool
	mode         configTeamsMode
	input        textinput.Model
	confirmMsg   string
}

// NewConfigTeamsView creates a ConfigTeamsView.
func NewConfigTeamsView(c *client.Client) *ConfigTeamsView {
	ti := textinput.New()
	ti.Width = 40
	return &ConfigTeamsView{c: c, loading: true, input: ti}
}

// Init implements tea.Model.
func (v *ConfigTeamsView) Init() tea.Cmd {
	return v.loadTeams()
}

func (v *ConfigTeamsView) loadTeams() tea.Cmd {
	return func() tea.Msg {
		teams, err := v.c.GetTeams()
		return teamsLoadedMsg{teams: teams, err: err}
	}
}

func (v *ConfigTeamsView) currentTeam() *client.TeamItem {
	if v.teamCursor < 0 || v.teamCursor >= len(v.teams) {
		return nil
	}
	return &v.teams[v.teamCursor]
}

func (v *ConfigTeamsView) currentMember() *client.TeamMemberItem {
	t := v.currentTeam()
	if t == nil || !v.expanded {
		return nil
	}
	if v.memberCursor < 0 || v.memberCursor >= len(t.Members) {
		return nil
	}
	return &t.Members[v.memberCursor]
}

// Update implements tea.Model.
func (v *ConfigTeamsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case teamsLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.teams = m.teams
		}
		return v, nil

	case teamsReloadMsg:
		v.loading = true
		return v, v.loadTeams()

	case teamsMutatedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		}
		v.loading = true
		return v, v.loadTeams()

	case tea.KeyMsg:
		return v.handleKey(m.String())
	}
	return v, nil
}

func (v *ConfigTeamsView) handleKey(key string) (tea.Model, tea.Cmd) {
	switch v.mode {
	case cfgTeamsModeInputNewTeam, cfgTeamsModeInputEditTeam,
		cfgTeamsModeInputNewMember, cfgTeamsModeInputEditMember,
		cfgTeamsModeInputMarketingLabel:
		return v.handleInputKey(key)
	case cfgTeamsModeConfirmDeleteTeam, cfgTeamsModeConfirmDeleteMember:
		return v.handleConfirmKey(key)
	default:
		return v.handleNormalKey(key)
	}
}

func (v *ConfigTeamsView) handleInputKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		name := strings.TrimSpace(v.input.Value())
		if name == "" {
			v.mode = cfgTeamsModeNormal
			return v, nil
		}
		mode := v.mode
		v.mode = cfgTeamsModeNormal
		return v, v.submitInput(mode, name)
	case "esc":
		v.mode = cfgTeamsModeNormal
		return v, nil
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	if key == "backspace" {
		v.input, cmd = v.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	return v, cmd
}

func (v *ConfigTeamsView) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	mode := v.mode
	v.mode = cfgTeamsModeNormal
	v.confirmMsg = ""
	if key == "y" || key == "Y" {
		return v, v.executeDelete(mode)
	}
	return v, nil
}

func (v *ConfigTeamsView) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	if v.expanded {
		return v.handleExpandedKey(key)
	}
	return v.handleTeamListKey(key)
}

func (v *ConfigTeamsView) handleTeamListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if v.teamCursor < len(v.teams)-1 {
			v.teamCursor++
		}
	case "k", "up":
		if v.teamCursor > 0 {
			v.teamCursor--
		}
	case "enter":
		if t := v.currentTeam(); t != nil {
			v.expanded = true
			v.memberCursor = 0
		}
	case "n":
		v.input.SetValue("")
		v.input.Focus()
		v.mode = cfgTeamsModeInputNewTeam
	case "e":
		if t := v.currentTeam(); t != nil {
			v.input.SetValue(t.Name)
			v.input.Focus()
			v.mode = cfgTeamsModeInputEditTeam
		}
	case "d":
		if t := v.currentTeam(); t != nil {
			v.confirmMsg = fmt.Sprintf("Delete team %q? [y/N]", t.Name)
			v.mode = cfgTeamsModeConfirmDeleteTeam
		}
	case "esc":
		return v, func() tea.Msg { return msgs.PopViewMsg{} }
	}
	return v, nil
}

func (v *ConfigTeamsView) handleExpandedKey(key string) (tea.Model, tea.Cmd) {
	t := v.currentTeam()
	switch key {
	case "j", "down":
		if t != nil && v.memberCursor < len(t.Members)-1 {
			v.memberCursor++
		}
	case "k", "up":
		if v.memberCursor > 0 {
			v.memberCursor--
		}
	case "esc":
		v.expanded = false
		v.memberCursor = 0
	case "n":
		v.input.SetValue("")
		v.input.Focus()
		v.mode = cfgTeamsModeInputNewMember
	case "e":
		if m := v.currentMember(); m != nil {
			v.input.SetValue(m.DisplayName)
			v.input.Focus()
			v.mode = cfgTeamsModeInputEditMember
		}
	case "d":
		if m := v.currentMember(); m != nil {
			v.confirmMsg = fmt.Sprintf("Delete member %q? [y/N]", m.DisplayName)
			v.mode = cfgTeamsModeConfirmDeleteMember
		}
	case "m":
		if t := v.currentTeam(); t != nil {
			current := ""
			if t.MarketingLabel != nil {
				current = *t.MarketingLabel
			}
			v.input.SetValue(current)
			v.input.Focus()
			v.mode = cfgTeamsModeInputMarketingLabel
		}
	case "s":
		if t := v.currentTeam(); t != nil {
			sv := NewConfigTeamSlotsView(v.c, t.ID, t.Name)
			return v, func() tea.Msg { return msgs.PushViewMsg{View: sv} }
		}
	}
	return v, nil
}

func (v *ConfigTeamsView) submitInput(mode configTeamsMode, name string) tea.Cmd {
	c := v.c
	switch mode {
	case cfgTeamsModeInputNewTeam:
		return func() tea.Msg {
			_, err := c.PostConfigTeam(name)
			return teamsMutatedMsg{err: err}
		}
	case cfgTeamsModeInputEditTeam:
		t := v.currentTeam()
		if t == nil {
			return nil
		}
		id := t.ID
		return func() tea.Msg {
			_, err := c.PutConfigTeam(id, name)
			return teamsMutatedMsg{err: err}
		}
	case cfgTeamsModeInputNewMember:
		t := v.currentTeam()
		if t == nil {
			return nil
		}
		teamID := t.ID
		return func() tea.Msg {
			_, err := c.PostConfigMember(teamID, name, nil, nil)
			return teamsMutatedMsg{err: err}
		}
	case cfgTeamsModeInputEditMember:
		m := v.currentMember()
		if m == nil {
			return nil
		}
		memberID := m.ID
		return func() tea.Msg {
			err := c.PutConfigMember(memberID, name, nil, nil)
			return teamsMutatedMsg{err: err}
		}
	case cfgTeamsModeInputMarketingLabel:
		t := v.currentTeam()
		if t == nil {
			return nil
		}
		teamID := t.ID
		return func() tea.Msg {
			err := c.PutTeamMarketingLabel(teamID, name)
			return teamsMutatedMsg{err: err}
		}
	}
	return nil
}

func (v *ConfigTeamsView) executeDelete(mode configTeamsMode) tea.Cmd {
	c := v.c
	switch mode {
	case cfgTeamsModeConfirmDeleteTeam:
		t := v.currentTeam()
		if t == nil {
			return nil
		}
		id := t.ID
		return func() tea.Msg {
			err := c.DeleteConfigTeam(id)
			return teamsMutatedMsg{err: err}
		}
	case cfgTeamsModeConfirmDeleteMember:
		m := v.currentMember()
		if m == nil {
			return nil
		}
		id := m.ID
		return func() tea.Msg {
			err := c.DeleteConfigMember(id)
			return teamsMutatedMsg{err: err}
		}
	}
	return nil
}

// View implements tea.Model.
func (v *ConfigTeamsView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config — Teams & Members") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
		v.errMsg = ""
	}

	// Input mode overlay.
	switch v.mode {
	case cfgTeamsModeInputNewTeam:
		sb.WriteString("  New team name: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to confirm  ·  Esc to cancel") + "\n\n")
	case cfgTeamsModeInputEditTeam:
		sb.WriteString("  Rename team: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to confirm  ·  Esc to cancel") + "\n\n")
	case cfgTeamsModeInputNewMember:
		sb.WriteString("  New member name: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to confirm  ·  Esc to cancel") + "\n\n")
	case cfgTeamsModeInputEditMember:
		sb.WriteString("  Rename member: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to confirm  ·  Esc to cancel") + "\n\n")
	case cfgTeamsModeInputMarketingLabel:
		sb.WriteString("  Marketing label: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to confirm  ·  Esc to cancel  ·  Leave empty to clear") + "\n\n")
	case cfgTeamsModeConfirmDeleteTeam, cfgTeamsModeConfirmDeleteMember:
		sb.WriteString("  " + v.confirmMsg + "\n\n")
	}

	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}
	if len(v.teams) == 0 {
		sb.WriteString("  No teams configured. Press n to create one.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	for i, t := range v.teams {
		teamPrefix := "  "
		teamLabel := t.Name
		if i == v.teamCursor {
			teamPrefix = "> "
			if !v.expanded {
				teamLabel = cfgSelectedStyle.Render(t.Name)
			}
		}
		arrow := " "
		if i == v.teamCursor && v.expanded {
			arrow = "▼"
		} else if i == v.teamCursor {
			arrow = "▶"
		}
		mktSuffix := ""
		if t.MarketingLabel != nil && *t.MarketingLabel != "" {
			mktSuffix = "  mkt:" + *t.MarketingLabel
		}
		sb.WriteString(fmt.Sprintf("%s%s %s  %s\n", teamPrefix, arrow, teamLabel, cfgDimStyle.Render(fmt.Sprintf("(%d members)%s", len(t.Members), mktSuffix))))

		if i == v.teamCursor && v.expanded {
			if len(t.Members) == 0 {
				sb.WriteString("      " + cfgDimStyle.Render("(no members)") + "\n")
			}
			for j, mem := range t.Members {
				memPrefix := "      "
				memLabel := mem.DisplayName
				if j == v.memberCursor {
					memPrefix = "    > "
					memLabel = cfgSelectedStyle.Render(mem.DisplayName)
				}
				extra := ""
				if mem.GithubUsername != nil {
					extra += " @" + *mem.GithubUsername
				}
				sb.WriteString(fmt.Sprintf("%s%s%s\n", memPrefix, memLabel, cfgDimStyle.Render(extra)))
			}
		}
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *ConfigTeamsView) footer() string {
	if v.expanded {
		return "\n" + cfgDimStyle.Render("  j/k navigate members  ·  n add member  ·  e edit  ·  d delete  ·  m marketing label  ·  s sources  ·  Esc collapse") + "\n"
	}
	return "\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter expand  ·  n new team  ·  e rename  ·  d delete  ·  Esc back") + "\n"
}

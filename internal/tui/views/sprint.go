package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/your-org/dashboard/internal/tui/client"
)

// warningAmberStyle is used for amber/orange warnings in the sprint view.
var warningAmberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

// sprintLoadedMsg is sent when sprint data has been fetched.
type sprintLoadedMsg struct {
	data *client.SprintResponse
	err  error
}

// SprintView shows sprint & plan status for a team.
type SprintView struct {
	c        *client.Client
	teamID   int64
	teamName string
	data     *client.SprintResponse
	loading  bool
	errMsg   string
}

// NewSprintView creates a SprintView for the given team.
func NewSprintView(c *client.Client, teamID int64, teamName string) *SprintView {
	return &SprintView{c: c, teamID: teamID, teamName: teamName, loading: true}
}

// Init implements tea.Model — load sprint data immediately.
func (v *SprintView) Init() tea.Cmd {
	return v.loadData()
}

func (v *SprintView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetSprint(v.teamID)
		return sprintLoadedMsg{data: data, err: err}
	}
}

// Update implements tea.Model.
func (v *SprintView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case sprintLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.data = m.data
		}
		return v, nil
	}
	return v, nil
}

// View implements tea.Model.
func (v *SprintView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render(v.teamName+" — Sprint & Plan Status") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.errMsg) + "\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	if v.data == nil {
		sb.WriteString("  No data yet. Go back and press r to sync this team.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("  Week %d of %d\n\n", v.data.CurrentSprint, v.data.TotalSprints))

	if v.data.StartDateMissing {
		sb.WriteString(warningAmberStyle.Render("  ⚠  Sprint start date not found. Add it to the plan document or annotate it in Config.") + "\n\n")
	}
	if v.data.NextPlanStartRisk {
		msg := fmt.Sprintf("  ✗  Current plan extended to sprint %d. This delays the next plan's start.", v.data.TotalSprints)
		sb.WriteString(errorStyle.Render(msg) + "\n\n")
	}

	if len(v.data.Goals) > 0 {
		sb.WriteString("  Goals:\n")
		for _, g := range v.data.Goals {
			sb.WriteString("    • " + g + "\n")
		}
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *SprintView) footer() string {
	lastSync := "Never synced"
	if v.data != nil && v.data.LastSyncedAt != nil {
		lastSync = "Last synced: " + *v.data.LastSyncedAt
	}
	return "\n" + dimStyle.Render("  "+lastSync+"  ·  Esc to go back") + "\n"
}

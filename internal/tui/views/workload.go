package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// workloadLoadedMsg is sent when workload data has been fetched.
type workloadLoadedMsg struct {
	data *client.WorkloadResponse
	err  error
}

// WorkloadView shows per-member workload for a team.
type WorkloadView struct {
	c        *client.Client
	teamID   int64
	teamName string
	data     *client.WorkloadResponse
	loading  bool
	errMsg   string
	cursor   int
}

// NewWorkloadView creates a WorkloadView for the given team.
func NewWorkloadView(c *client.Client, teamID int64, teamName string) *WorkloadView {
	return &WorkloadView{c: c, teamID: teamID, teamName: teamName, loading: true}
}

// Init implements tea.Model — load data immediately.
func (v *WorkloadView) Init() tea.Cmd {
	return v.loadData()
}

func (v *WorkloadView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetWorkload(v.teamID)
		return workloadLoadedMsg{data: data, err: err}
	}
}

// Update implements tea.Model.
func (v *WorkloadView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case workloadLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.data = m.data
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.data != nil && v.cursor < len(v.data.Members)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "a":
			return v, v.pushAnnotate()
		}
	}
	return v, nil
}

func (v *WorkloadView) pushAnnotate() tea.Cmd {
	// Workload annotations are team-level (no specific item ref).
	av := NewSectionAnnotateView(v.c, v.teamID, "team", "", nil)
	return func() tea.Msg { return PushViewMsg{View: av} }
}

// View implements tea.Model.
func (v *WorkloadView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render(v.teamName+" — Resource / Workload") + "\n\n")

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
	if v.data == nil || len(v.data.Members) == 0 {
		sb.WriteString("  No data yet. Go back and press r to sync this team.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("  %-24s  %-10s  %s\n", "Member", "Est. Days", "Load"))
	sb.WriteString(dimStyle.Render("  "+strings.Repeat("─", 50)) + "\n")

	for i, m := range v.data.Members {
		var labelStyle = riskNormalStyle
		switch strings.ToUpper(m.Label) {
		case "HIGH":
			labelStyle = riskHighStyle
		case "LOW":
			labelStyle = dimStyle
		}
		label := labelStyle.Render(fmt.Sprintf("[%s]", strings.ToUpper(m.Label)))

		row := fmt.Sprintf("  %-24s  %-10s  %s", m.Name, fmt.Sprintf("%.1f days", m.EstimatedDays), label)
		if i == v.cursor {
			sb.WriteString(selectedStyle.Render(">") + row[1:] + "\n")
		} else {
			sb.WriteString(row + "\n")
		}
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *WorkloadView) footer() string {
	lastSync := "Never synced"
	if v.data != nil && v.data.LastSyncedAt != nil {
		lastSync = "Last synced: " + *v.data.LastSyncedAt
	}
	return "\n" + dimStyle.Render("  "+lastSync+"  ·  j/k navigate  ·  a to annotate (team)  ·  Esc to go back") + "\n"
}

package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// sparklineBlocks are the unicode block characters for the sparkline.
var sparklineBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// velocityLoadedMsg is sent when velocity data has been fetched.
type velocityLoadedMsg struct {
	data *client.VelocityResponse
	err  error
}

// VelocityView shows sprint velocity over recent sprints.
type VelocityView struct {
	c        *client.Client
	teamID   int64
	teamName string
	data     *client.VelocityResponse
	loading  bool
	errMsg   string
}

// NewVelocityView creates a VelocityView for the given team.
func NewVelocityView(c *client.Client, teamID int64, teamName string) *VelocityView {
	return &VelocityView{c: c, teamID: teamID, teamName: teamName, loading: true}
}

// Init implements tea.Model — load data immediately.
func (v *VelocityView) Init() tea.Cmd {
	return v.loadData()
}

func (v *VelocityView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetVelocity(v.teamID)
		return velocityLoadedMsg{data: data, err: err}
	}
}

// Update implements tea.Model.
func (v *VelocityView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case velocityLoadedMsg:
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

// sparkline builds a unicode bar chart string from the given scores.
func sparkline(sprints []client.VelocitySprint) string {
	if len(sprints) == 0 {
		return ""
	}
	// Find max score for normalization.
	max := sprints[0].Score
	for _, s := range sprints[1:] {
		if s.Score > max {
			max = s.Score
		}
	}
	var sb strings.Builder
	for _, s := range sprints {
		idx := 0
		if max > 0 {
			idx = int(s.Score / max * float64(len(sparklineBlocks)-1))
			if idx >= len(sparklineBlocks) {
				idx = len(sparklineBlocks) - 1
			}
		}
		sb.WriteRune(sparklineBlocks[idx])
	}
	return sb.String()
}

// View implements tea.Model.
func (v *VelocityView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render(v.teamName+" — Velocity") + "\n\n")

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
	if v.data == nil || len(v.data.Sprints) == 0 {
		sb.WriteString("  No data yet. Go back and press r to sync this team.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	// Sparkline.
	chart := sparkline(v.data.Sprints)
	sb.WriteString("  " + selectedStyle.Render(chart) + "\n\n")

	// Tabular breakdown.
	sb.WriteString(fmt.Sprintf("  %-16s  %6s  %8s  %6s  %7s\n", "Sprint", "Score", "Issues", "PRs", "Commits"))
	sb.WriteString(dimStyle.Render("  "+strings.Repeat("─", 52)) + "\n")
	for _, sp := range v.data.Sprints {
		sb.WriteString(fmt.Sprintf("  %-16s  %6.1f  %8.0f  %6.0f  %7.0f\n",
			sp.Label,
			sp.Score,
			sp.Breakdown.Issues,
			sp.Breakdown.PRs,
			sp.Breakdown.Commits,
		))
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *VelocityView) footer() string {
	lastSync := "Never synced"
	if v.data != nil && v.data.LastSyncedAt != nil {
		lastSync = "Last synced: " + *v.data.LastSyncedAt
	}
	return "\n" + dimStyle.Render("  "+lastSync+"  ·  Esc to go back") + "\n"
}

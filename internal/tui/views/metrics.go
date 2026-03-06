package views

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// metricsLoadedMsg is sent when metrics data has been fetched.
type metricsLoadedMsg struct {
	data *client.MetricsResponse
	err  error
}

// MetricsView shows business metric panels for a team.
type MetricsView struct {
	c        *client.Client
	teamID   int64
	teamName string
	data     *client.MetricsResponse
	loading  bool
	errMsg   string
}

// NewMetricsView creates a MetricsView for the given team.
func NewMetricsView(c *client.Client, teamID int64, teamName string) *MetricsView {
	return &MetricsView{c: c, teamID: teamID, teamName: teamName, loading: true}
}

// Init implements tea.Model — load data immediately.
func (v *MetricsView) Init() tea.Cmd {
	return v.loadData()
}

func (v *MetricsView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetMetrics(v.teamID)
		return metricsLoadedMsg{data: data, err: err}
	}
}

// Update implements tea.Model.
func (v *MetricsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case metricsLoadedMsg:
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
func (v *MetricsView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render(v.teamName+" — Business Metrics") + "\n\n")

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
	if v.data == nil || len(v.data.Panels) == 0 {
		sb.WriteString("  No data yet. Go back and press r to sync this team.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	for _, p := range v.data.Panels {
		value := dimStyle.Render("—")
		if p.Value != nil {
			value = *p.Value
		}
		sb.WriteString("  " + selectedStyle.Render(p.Title) + "  " + value + "\n")
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *MetricsView) footer() string {
	lastSync := "Never synced"
	if v.data != nil && v.data.LastSyncedAt != nil {
		lastSync = "Last synced: " + *v.data.LastSyncedAt
	}
	return "\n" + dimStyle.Render("  "+lastSync+"  ·  Esc to go back") + "\n"
}

package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/your-org/dashboard/internal/tui/client"
)

// ---- internal message types ----

type orgLoadedMsg struct {
	data *client.OrgOverviewResponse
	err  error
}

type orgSyncStartedMsg struct {
	runID int64
	err   error
}

type orgSyncPollMsg struct{ runID int64 }

type orgSyncDoneMsg struct {
	status string
	errMsg string
}

// ---- styles ----

var (
	riskHighStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	riskMediumStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	riskLowStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	riskNormalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	selectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	dimStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	syncBannerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// OrgOverviewView shows all teams at a glance.
type OrgOverviewView struct {
	c       *client.Client
	data    *client.OrgOverviewResponse
	cursor  int
	loading bool
	errMsg  string
	syncing bool
	syncMsg string
}

// NewOrgOverviewView creates the org overview view.
func NewOrgOverviewView(c *client.Client) *OrgOverviewView {
	return &OrgOverviewView{c: c, loading: true}
}

// Init implements tea.Model — load org data immediately.
func (v *OrgOverviewView) Init() tea.Cmd {
	return v.loadData()
}

func (v *OrgOverviewView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetOrgOverview()
		return orgLoadedMsg{data: data, err: err}
	}
}

func doOrgSync(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		runID, err := c.PostSync("org", nil)
		return orgSyncStartedMsg{runID: runID, err: err}
	}
}

func pollOrgSync(c *client.Client, runID int64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return orgSyncDoneMsg{status: "error", errMsg: err.Error()}
		}
		if run.Status == "done" || run.Status == "error" {
			errDetail := ""
			if run.Error != nil {
				errDetail = *run.Error
			}
			return orgSyncDoneMsg{status: run.Status, errMsg: errDetail}
		}
		return orgSyncPollMsg{runID: runID}
	}
}

// Update implements tea.Model.
func (v *OrgOverviewView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case orgLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.data = m.data
			v.errMsg = ""
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.data != nil && v.cursor < len(v.data.Teams)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "enter":
			if v.data != nil && len(v.data.Teams) > 0 {
				team := v.data.Teams[v.cursor]
				tv := NewTeamView(v.c, team.ID, team.Name)
				return v, func() tea.Msg {
					return PushViewMsg{View: tv}
				}
			}
			return v, nil
		case "R":
			if !v.syncing {
				v.syncing = true
				v.syncMsg = "Syncing org…"
				return v, doOrgSync(v.c)
			}
			return v, nil
		}

	case orgSyncStartedMsg:
		if m.err != nil {
			v.syncing = false
			v.syncMsg = ""
			v.errMsg = "Sync failed: " + m.err.Error()
		}
		return v, pollOrgSync(v.c, m.runID)

	case orgSyncPollMsg:
		return v, pollOrgSync(v.c, m.runID)

	case orgSyncDoneMsg:
		v.syncing = false
		if m.status == "error" && m.errMsg != "" {
			v.syncMsg = "Sync error: " + m.errMsg
		} else {
			v.syncMsg = ""
		}
		// Reload data after sync.
		v.loading = true
		return v, v.loadData()
	}

	return v, nil
}

// View implements tea.Model.
func (v *OrgOverviewView) View() string {
	var sb strings.Builder

	// Sync banner
	if v.syncing && v.syncMsg != "" {
		sb.WriteString(syncBannerStyle.Render("  "+v.syncMsg) + "\n\n")
	} else if v.syncMsg != "" {
		sb.WriteString(syncBannerStyle.Render("  "+v.syncMsg) + "\n\n")
	}

	// Error
	if v.errMsg != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.errMsg) + "\n\n")
	}

	// Loading
	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	// No data
	if v.data == nil || len(v.data.Teams) == 0 {
		sb.WriteString("  No data yet. Press r to sync this team (or R for full org).\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	// Team cards
	sb.WriteString("\n")
	for i, team := range v.data.Teams {
		prefix := "  "
		name := team.Name
		if i == v.cursor {
			prefix = "> "
			name = selectedStyle.Render(name)
		}

		sprint := fmt.Sprintf("Sprint %d/%d", team.CurrentSprint, team.TotalSprints)
		risk := renderRisk(team.RiskLevel)
		focus := team.Focus
		if len(focus) > 50 {
			focus = focus[:47] + "..."
		}

		line := fmt.Sprintf("%s%-20s  %-14s  |  Risk: %-20s  |  Focus: %s",
			prefix, name, sprint, risk, focus)
		sb.WriteString(line + "\n")
	}

	// Org metrics summary
	if len(v.data.Workload) > 0 {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("  Org Workload:"))
		parts := make([]string, 0, len(v.data.Workload))
		for _, w := range v.data.Workload {
			parts = append(parts, fmt.Sprintf("%s %.1fd [%s]", w.Name, w.TotalDays, w.Label))
		}
		sb.WriteString("  " + strings.Join(parts, "   ") + "\n")
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *OrgOverviewView) footer() string {
	lastSync := "Never synced"
	if v.data != nil && v.data.LastSyncedAt != nil {
		lastSync = "Last synced: " + *v.data.LastSyncedAt
	}
	return "\n" + dimStyle.Render("  "+lastSync+"  ·  j/k navigate  ·  Enter to drill in  ·  R to sync org  ·  q to quit") + "\n"
}

func renderRisk(level string) string {
	switch strings.ToUpper(level) {
	case "HIGH":
		return riskHighStyle.Render("HIGH")
	case "MEDIUM":
		return riskMediumStyle.Render("MEDIUM")
	case "LOW":
		return riskLowStyle.Render("LOW")
	default:
		return riskNormalStyle.Render(level)
	}
}

// PushViewMsg is sent by any view to push a new view onto the App stack.
// Defined here to avoid an import cycle (tui imports views).
type PushViewMsg struct{ View tea.Model }

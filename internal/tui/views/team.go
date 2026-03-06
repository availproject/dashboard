package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// ---- internal message types ----

type teamSyncStartedMsg struct {
	runID int64
	err   error
}

type teamSyncPollMsg struct{ runID int64 }

type teamSyncDoneMsg struct {
	status string
	errMsg string
}

// teamMenuItems are the team drill-down sub-menu options.
var teamMenuItems = []string{
	"Sprint & Plan Status",
	"Goals & Concerns",
	"Resource/Workload",
	"Velocity",
	"Business Metrics",
}

// TeamView is the team drill-down view with a sub-menu.
type TeamView struct {
	c       *client.Client
	teamID  int64
	name    string
	cursor  int
	syncing bool
	syncMsg string
	errMsg  string
}

// NewTeamView creates a TeamView for the given team.
func NewTeamView(c *client.Client, teamID int64, name string) *TeamView {
	return &TeamView{c: c, teamID: teamID, name: name}
}

// Init implements tea.Model.
func (v *TeamView) Init() tea.Cmd { return nil }

func doTeamSync(c *client.Client, teamID int64) tea.Cmd {
	return func() tea.Msg {
		runID, err := c.PostSync("team", &teamID)
		return teamSyncStartedMsg{runID: runID, err: err}
	}
}

func pollTeamSync(c *client.Client, runID int64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return teamSyncDoneMsg{status: "error", errMsg: err.Error()}
		}
		if run.Status == "done" || run.Status == "error" {
			errDetail := ""
			if run.Error != nil {
				errDetail = *run.Error
			}
			return teamSyncDoneMsg{status: run.Status, errMsg: errDetail}
		}
		return teamSyncPollMsg{runID: runID}
	}
}

// Update implements tea.Model.
func (v *TeamView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.cursor < len(teamMenuItems)-1 {
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
		case "r":
			if !v.syncing {
				v.syncing = true
				v.syncMsg = "Syncing team…"
				return v, doTeamSync(v.c, v.teamID)
			}
			return v, nil
		}

	case teamSyncStartedMsg:
		if m.err != nil {
			v.syncing = false
			v.syncMsg = ""
			v.errMsg = "Sync failed: " + m.err.Error()
			return v, nil
		}
		return v, pollTeamSync(v.c, m.runID)

	case teamSyncPollMsg:
		return v, pollTeamSync(v.c, m.runID)

	case teamSyncDoneMsg:
		v.syncing = false
		if m.status == "error" && m.errMsg != "" {
			v.syncMsg = "Sync error: " + m.errMsg
		} else {
			v.syncMsg = "Sync complete."
		}
		return v, nil
	}

	return v, nil
}

func (v *TeamView) pushSubView() tea.Cmd {
	var subView tea.Model
	switch v.cursor {
	case 0:
		subView = NewSprintView(v.c, v.teamID, v.name)
	case 1:
		subView = NewGoalsView(v.c, v.teamID, v.name)
	case 2:
		subView = NewWorkloadView(v.c, v.teamID, v.name)
	case 3:
		subView = NewVelocityView(v.c, v.teamID, v.name)
	case 4:
		subView = NewMetricsView(v.c, v.teamID, v.name)
	default:
		return nil
	}
	return func() tea.Msg { return PushViewMsg{View: subView} }
}

// View implements tea.Model.
func (v *TeamView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render(v.name) + "\n\n")

	if v.syncMsg != "" {
		style := syncBannerStyle
		if strings.HasPrefix(v.syncMsg, "Sync error") {
			style = errorStyle
		}
		sb.WriteString(style.Render("  "+v.syncMsg) + "\n\n")
	}
	if v.errMsg != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.errMsg) + "\n\n")
	}

	for i, item := range teamMenuItems {
		prefix := "  "
		label := item
		if i == v.cursor {
			prefix = "> "
			label = selectedStyle.Render(item)
		}
		sb.WriteString(fmt.Sprintf("%s%s\n", prefix, label))
	}

	sb.WriteString("\n" + dimStyle.Render("  j/k navigate  ·  Enter to select  ·  r to sync team  ·  Esc to go back") + "\n")
	return sb.String()
}

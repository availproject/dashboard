package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/components"
	"github.com/your-org/dashboard/internal/tui/msgs"
	"github.com/your-org/dashboard/internal/tui/views"
)

// ShowLoginMsg is sent when an API call fails with ErrUnauthenticated.
// The App will push a new LoginView on top of the current stack.
type ShowLoginMsg struct{}

// SyncStartedMsg is sent by a view when it successfully starts a sync run.
// App will show the banner and begin polling for completion.
type SyncStartedMsg struct {
	RunID int64
	Label string // e.g. team name or "org"
}

// SyncDoneMsg is sent by SyncPoller when a sync run finishes successfully.
type SyncDoneMsg struct {
	RunID int64
}

// SyncFailedMsg is sent by SyncPoller when a sync run fails.
type SyncFailedMsg struct {
	RunID int64
	Err   string
}

// syncPollMsg is an internal message that triggers the next poll tick.
type syncPollMsg struct {
	RunID int64
}

// SyncPoller polls GET /sync/{run_id} until the run completes.
type SyncPoller struct {
	client *client.Client
}

// Poll starts polling for the given sync run ID and returns a Bubble Tea Cmd.
func (p *SyncPoller) Poll(runID int64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := p.client.GetSyncRun(runID)
		if err != nil {
			return SyncFailedMsg{RunID: runID, Err: err.Error()}
		}
		if run.Status == "done" {
			return SyncDoneMsg{RunID: runID}
		}
		if run.Status == "error" {
			errDetail := ""
			if run.Error != nil {
				errDetail = *run.Error
			}
			return SyncFailedMsg{RunID: runID, Err: errDetail}
		}
		return syncPollMsg{RunID: runID}
	}
}

// App is the root Bubble Tea model. It manages a stack of views.
type App struct {
	views      []tea.Model
	client     *client.Client
	syncPoller *SyncPoller
	banner     components.Banner
	termWidth  int
	termHeight int
}

// NewApp creates an App with the given client. The views stack starts empty;
// the caller should push an initial view (e.g., login) before running.
func NewApp(c *client.Client) *App {
	return &App{
		client:     c,
		syncPoller: &SyncPoller{client: c},
	}
}

// PushView appends a view to the stack. Used by cmd/tui/main.go to set the initial view.
func (a *App) PushView(v tea.Model) {
	a.views = append(a.views, v)
}

// Client returns the HTTP client, so views can access it.
func (a *App) Client() *client.Client {
	return a.client
}

// SyncPollerInstance returns the SyncPoller, so views can start polling.
func (a *App) SyncPollerInstance() *SyncPoller {
	return a.syncPoller
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	if len(a.views) > 0 {
		return a.views[0].Init()
	}
	return nil
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		// Let the user dismiss a failed sync banner by pressing Enter.
		if a.banner.Active && a.banner.Failed && m.String() == "enter" {
			a.banner = components.Banner{}
			return a, nil
		}
		switch m.String() {
		case "q":
			top := a.views[len(a.views)-1]
			if v, ok := top.(interface{ InterceptsBackspace() bool }); ok && v.InterceptsBackspace() {
				break
			}
			return a, tea.Quit
		case "ctrl+c":
			return a, tea.Quit
		case "esc", "backspace":
			// Let views that own a text input or mode (e.g. AnnotateView, TeamReportView in annotate mode)
			// handle esc/backspace themselves.
			top := a.views[len(a.views)-1]
			if m.String() == "backspace" {
				if v, ok := top.(interface{ InterceptsBackspace() bool }); ok && v.InterceptsBackspace() {
					break
				}
			}
			if m.String() == "esc" {
				if v, ok := top.(interface{ InterceptsEsc() bool }); ok && v.InterceptsEsc() {
					break
				}
			}
			if len(a.views) > 1 {
				a.views = a.views[:len(a.views)-1]
				return a, nil
			}
		}

	case tea.WindowSizeMsg:
		a.termWidth = m.Width
		a.termHeight = m.Height
		// fall through to delegate to the top view

	case msgs.PushViewMsg:
		a.views = append(a.views, m.View)
		cmds := []tea.Cmd{m.View.Init()}
		if a.termWidth > 0 || a.termHeight > 0 {
			size := tea.WindowSizeMsg{Width: a.termWidth, Height: a.termHeight}
			updated, cmd := m.View.Update(size)
			a.views[len(a.views)-1] = updated
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return a, tea.Batch(cmds...)

	case msgs.PopViewMsg:
		if len(a.views) > 1 {
			a.views = a.views[:len(a.views)-1]
			return a, a.views[len(a.views)-1].Init()
		}
		return a, nil

	case views.LoginDoneMsg:
		// Pop the login view, then push the org overview.
		if len(a.views) > 0 {
			a.views = a.views[:len(a.views)-1]
		}
		ov := views.NewOrgOverviewView(a.client)
		a.views = append(a.views, ov)
		cmds := []tea.Cmd{ov.Init()}
		if a.termWidth > 0 || a.termHeight > 0 {
			size := tea.WindowSizeMsg{Width: a.termWidth, Height: a.termHeight}
			updated, cmd := ov.Update(size)
			a.views[len(a.views)-1] = updated
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return a, tea.Batch(cmds...)

	case ShowLoginMsg:
		lv := views.NewLoginView(a.client)
		a.views = append(a.views, lv)
		return a, lv.Init()

	case SyncStartedMsg:
		a.banner = components.Banner{Active: true, Label: m.Label}
		return a, a.syncPoller.Poll(m.RunID)

	case SyncDoneMsg:
		a.banner = components.Banner{}
		return a, nil

	case SyncFailedMsg:
		a.banner = components.Banner{Active: true, Failed: true, FailedMsg: m.Err}
		return a, nil

	case syncPollMsg:
		return a, a.syncPoller.Poll(m.RunID)
	}

	// Delegate remaining messages to the top view.
	if len(a.views) > 0 {
		top := a.views[len(a.views)-1]
		updated, cmd := top.Update(msg)
		a.views[len(a.views)-1] = updated
		return a, cmd
	}

	return a, nil
}

// View implements tea.Model.
func (a *App) View() string {
	if len(a.views) == 0 {
		return ""
	}
	return a.banner.Render() + a.views[len(a.views)-1].View()
}

package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// PushViewMsg is sent by any view to push a new view onto the stack.
type PushViewMsg struct {
	View tea.Model
}

// SyncDoneMsg is sent by SyncPoller when a sync run finishes.
type SyncDoneMsg struct {
	RunID  int64
	Status string
	Err    string
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
			return SyncDoneMsg{RunID: runID, Err: err.Error()}
		}
		if run.Status == "done" || run.Status == "error" {
			errDetail := ""
			if run.Error != nil {
				errDetail = *run.Error
			}
			return SyncDoneMsg{RunID: runID, Status: run.Status, Err: errDetail}
		}
		return syncPollMsg{RunID: runID}
	}
}

// App is the root Bubble Tea model. It manages a stack of views.
type App struct {
	views      []tea.Model
	client     *client.Client
	syncPoller *SyncPoller
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
		switch m.String() {
		case "q", "ctrl+c":
			// Quit only when at the root of the stack.
			if len(a.views) <= 1 {
				return a, tea.Quit
			}
		case "esc", "backspace":
			if len(a.views) > 1 {
				a.views = a.views[:len(a.views)-1]
				return a, nil
			}
		}

	case PushViewMsg:
		a.views = append(a.views, m.View)
		return a, m.View.Init()

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
	return a.views[len(a.views)-1].View()
}

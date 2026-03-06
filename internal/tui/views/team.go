package views

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// TeamView is the team drill-down view. Implemented in US-031.
type TeamView struct {
	c      *client.Client
	teamID int64
	name   string
}

// NewTeamView creates a TeamView for the given team.
func NewTeamView(c *client.Client, teamID int64, name string) *TeamView {
	return &TeamView{c: c, teamID: teamID, name: name}
}

// Init implements tea.Model.
func (v *TeamView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *TeamView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return v, nil
}

// View implements tea.Model.
func (v *TeamView) View() string {
	return "\n  Team: " + v.name + "\n\n  (Team view coming soon — press Esc to go back)\n"
}

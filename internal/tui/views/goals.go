package views

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// goalsLoadedMsg is sent when goals/concerns data has been fetched.
type goalsLoadedMsg struct {
	data *client.GoalsResponse
	err  error
}

// GoalsView shows goals and concerns for a team.
type GoalsView struct {
	c        *client.Client
	teamID   int64
	teamName string
	data     *client.GoalsResponse
	loading  bool
	errMsg   string
	cursor   int // indexes into combined list (goals first, then concerns)
}

// NewGoalsView creates a GoalsView for the given team.
func NewGoalsView(c *client.Client, teamID int64, teamName string) *GoalsView {
	return &GoalsView{c: c, teamID: teamID, teamName: teamName, loading: true}
}

// Init implements tea.Model — load data immediately.
func (v *GoalsView) Init() tea.Cmd {
	return v.loadData()
}

func (v *GoalsView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetGoals(v.teamID)
		return goalsLoadedMsg{data: data, err: err}
	}
}

func (v *GoalsView) totalItems() int {
	if v.data == nil {
		return 0
	}
	return len(v.data.BusinessGoals) + len(v.data.SprintGoals) + len(v.data.Concerns)
}

// Update implements tea.Model.
func (v *GoalsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case goalsLoadedMsg:
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
			if v.cursor < v.totalItems()-1 {
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

func (v *GoalsView) pushAnnotate() tea.Cmd {
	if v.data == nil || v.totalItems() == 0 {
		return nil
	}
	nBiz := len(v.data.BusinessGoals)
	nSprint := len(v.data.SprintGoals)
	var itemRef, label string
	switch {
	case v.cursor < nBiz:
		g := v.data.BusinessGoals[v.cursor]
		itemRef = g.Text
		label = g.Text
	case v.cursor < nBiz+nSprint:
		g := v.data.SprintGoals[v.cursor-nBiz]
		itemRef = g.Text
		label = g.Text
	default:
		c := v.data.Concerns[v.cursor-nBiz-nSprint]
		itemRef = c.Key
		label = c.Summary
	}
	av := NewAnnotateView(v.c, v.teamID, "item", itemRef, label)
	return func() tea.Msg { return PushViewMsg{View: av} }
}

// View implements tea.Model.
func (v *GoalsView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render(v.teamName+" — Goals & Concerns") + "\n\n")

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

	idx := 0

	// Business goals section
	sb.WriteString(selectedStyle.Render("  Business Goals") + "\n")
	if len(v.data.BusinessGoals) == 0 {
		sb.WriteString(dimStyle.Render("    (none)") + "\n")
	} else {
		for _, g := range v.data.BusinessGoals {
			prefix := "    "
			badge := goalStatusBadge(g.Status)
			text := badge + " " + g.Text
			if idx == v.cursor {
				prefix = "  > "
				text = selectedStyle.Render(badge + " " + g.Text)
			}
			sb.WriteString(prefix + text + "\n")
			idx++
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// Sprint goals section
	sb.WriteString(selectedStyle.Render("  Sprint Goals") + "\n")
	if len(v.data.SprintGoals) == 0 {
		sb.WriteString(dimStyle.Render("    (none)") + "\n")
	} else {
		for _, g := range v.data.SprintGoals {
			prefix := "    "
			badge := sprintGoalStatusBadge(g.Status)
			text := badge + " " + g.Text
			if idx == v.cursor {
				prefix = "  > "
				text = selectedStyle.Render(badge + " " + g.Text)
			}
			sb.WriteString(prefix + text + "\n")
			idx++
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// Concerns section
	sb.WriteString(selectedStyle.Render("  Concerns") + "\n")
	if len(v.data.Concerns) == 0 {
		sb.WriteString(dimStyle.Render("    (none)") + "\n")
	} else {
		for _, c := range v.data.Concerns {
			selected := idx == v.cursor
			idx++

			prefix := "    "
			var severityStr string
			if strings.HasPrefix(c.Key, "stale_annotation_") {
				severityStr = warningAmberStyle.Render("[STALE ANNOTATION]")
			} else {
				switch strings.ToUpper(c.Severity) {
				case "HIGH":
					severityStr = riskHighStyle.Render("[HIGH]")
				case "MEDIUM":
					severityStr = riskMediumStyle.Render("[MEDIUM]")
				case "LOW":
					severityStr = dimStyle.Render("[LOW]")
				default:
					severityStr = "[" + c.Severity + "]"
				}
			}

			summary := c.Summary
			if selected {
				prefix = "  > "
				summary = selectedStyle.Render(summary)
			}
			sb.WriteString(prefix + severityStr + " " + summary + "\n")
			if c.Explanation != "" {
				sb.WriteString("      " + dimStyle.Render(c.Explanation) + "\n")
			}
		}
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *GoalsView) footer() string {
	lastSync := "Never synced"
	if v.data != nil && v.data.LastSyncedAt != nil {
		lastSync = "Last synced: " + *v.data.LastSyncedAt
	}
	return "\n" + dimStyle.Render("  "+lastSync+"  ·  j/k navigate  ·  a to annotate  ·  Esc to go back") + "\n"
}

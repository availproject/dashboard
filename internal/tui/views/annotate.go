package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// PopViewMsg is sent to pop the current view from the App stack.
type PopViewMsg struct{}

// annotateSubmitMsg is sent when the annotation POST completes.
type annotateSubmitMsg struct{ err error }

// AnnotateView is a pushed view for creating an annotation on an item.
type AnnotateView struct {
	c       *client.Client
	teamID  int64
	tier    string // "item" or "team"
	itemRef string
	label   string
	ta      textarea.Model
	loading bool
	errMsg  string
}

// NewAnnotateView creates an AnnotateView. tier should be "item" or "team".
func NewAnnotateView(c *client.Client, teamID int64, tier, itemRef, label string) *AnnotateView {
	ta := textarea.New()
	ta.Placeholder = "Enter annotation…"
	ta.Focus()
	ta.SetWidth(60)
	ta.SetHeight(5)
	return &AnnotateView{
		c:       c,
		teamID:  teamID,
		tier:    tier,
		itemRef: itemRef,
		label:   label,
		ta:      ta,
	}
}

// Init implements tea.Model.
func (v *AnnotateView) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model.
func (v *AnnotateView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		if v.loading {
			return v, nil
		}
		switch m.String() {
		case "tab":
			if v.tier == "item" {
				v.tier = "team"
			} else {
				v.tier = "item"
			}
			return v, nil
		case "ctrl+enter":
			content := strings.TrimSpace(v.ta.Value())
			if content == "" {
				v.errMsg = "annotation cannot be empty"
				return v, nil
			}
			v.loading = true
			v.errMsg = ""
			return v, v.doSubmit(content)
		}

	case annotateSubmitMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return PopViewMsg{} }
	}

	// Forward all other messages to the textarea.
	var cmd tea.Cmd
	v.ta, cmd = v.ta.Update(msg)
	return v, cmd
}

func (v *AnnotateView) doSubmit(content string) tea.Cmd {
	tid := v.teamID
	itemRef := v.itemRef
	tier := v.tier
	return func() tea.Msg {
		var itemRefPtr *string
		if tier == "item" && itemRef != "" {
			itemRefPtr = &itemRef
		}
		_, err := v.c.PostAnnotation(tier, &tid, itemRefPtr, content)
		return annotateSubmitMsg{err: err}
	}
}

// View implements tea.Model.
func (v *AnnotateView) View() string {
	var sb strings.Builder

	sb.WriteString("\n  " + selectedStyle.Render("Annotate") + "\n\n")

	label := v.label
	if len(label) > 70 {
		label = label[:67] + "..."
	}
	sb.WriteString("  Item: " + dimStyle.Render(label) + "\n\n")

	// Tier selector
	itemLabel := "[Item-level]"
	teamLabel := "[Team-level]"
	if v.tier == "item" {
		itemLabel = selectedStyle.Render(itemLabel)
		teamLabel = dimStyle.Render(teamLabel)
	} else {
		itemLabel = dimStyle.Render(itemLabel)
		teamLabel = selectedStyle.Render(teamLabel)
	}
	sb.WriteString("  Tier: " + itemLabel + "  " + teamLabel + "\n\n")

	sb.WriteString(v.ta.View() + "\n")

	if v.errMsg != "" {
		sb.WriteString("\n" + errorStyle.Render("  Error: "+v.errMsg) + "\n")
	}

	if v.loading {
		sb.WriteString("\n  Saving…\n")
	} else {
		sb.WriteString("\n" + dimStyle.Render("  Tab to toggle tier  ·  Ctrl+Enter to submit  ·  Esc to cancel") + "\n")
	}

	return sb.String()
}

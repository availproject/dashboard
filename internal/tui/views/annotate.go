package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// annotatePickItem describes one annotatable item on the team dashboard.
// itemRef holds the section key (e.g. "section:concerns", "" for team-level).
type annotatePickItem struct {
	tier    string // "item" or "team"
	itemRef string // section ref
	label   string // item display label for cursor in dashboard
}

// PopViewMsg is an alias kept for backward compatibility; use msgs.PopViewMsg directly.
type PopViewMsg = msgs.PopViewMsg

// ── Section annotation editor ─────────────────────────────────────────────────

type saMode int

const (
	saModeList saMode = iota
	saModeEdit
)

// sectionSavedMsg is sent when a POST/PUT annotation call completes.
type sectionSavedMsg struct {
	annotation client.SectionAnnotation
	editIdx    int // -1 = new item was appended
	err        error
}

// sectionDeletedMsg is sent when a DELETE annotation call completes.
type sectionDeletedMsg struct {
	idx int
	err error
}

// SectionAnnotateView is an editor for all annotations belonging to one section.
// It shows existing bullets in list mode and opens a textarea in edit mode.
type SectionAnnotateView struct {
	c          *client.Client
	teamID     int64
	tier       string // "item" or "team"
	sectionRef string // item_ref stored in DB; "" for team-level
	name       string // display name, e.g. "Concerns"
	items      []client.SectionAnnotation
	cursor     int    // 0..len(items); len(items) = [+ Add]
	mode       saMode
	editIdx    int // index being edited; -1 for a new annotation
	ta         textarea.Model
	loading    bool
	errMsg     string
}

// sectionName derives a human-readable name from a section ref.
func sectionName(ref string) string {
	switch ref {
	case "section:concerns":
		return "Concerns"
	case "section:sprint_goals":
		return "Sprint Goals"
	case "section:business_goals":
		return "Business Goals"
	default:
		return "Team"
	}
}

// NewSectionAnnotateView creates the editor for the given section.
func NewSectionAnnotateView(c *client.Client, teamID int64, tier, sectionRef string, existing []client.SectionAnnotation) *SectionAnnotateView {
	items := make([]client.SectionAnnotation, len(existing))
	copy(items, existing)
	return &SectionAnnotateView{
		c:          c,
		teamID:     teamID,
		tier:       tier,
		sectionRef: sectionRef,
		name:       sectionName(sectionRef),
		items:      items,
		cursor:     len(items), // start on [+ Add]
		editIdx:    -1,
	}
}

func newTA() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Enter annotation…"
	ta.Focus()
	ta.SetWidth(60)
	ta.SetHeight(4)
	return ta
}

// InterceptsBackspace tells the App not to treat backspace as navigation
// when the textarea is active.
func (v *SectionAnnotateView) InterceptsBackspace() bool {
	return v.mode == saModeEdit
}

// InterceptsEsc tells the App not to handle Esc directly — the view manages
// both edit-mode cancel (→ list) and list-mode close (→ PopViewMsg with Init).
func (v *SectionAnnotateView) InterceptsEsc() bool { return true }

func (v *SectionAnnotateView) Init() tea.Cmd { return nil }

func (v *SectionAnnotateView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		if v.loading {
			return v, nil
		}
		if v.mode == saModeEdit {
			return v.updateEdit(m)
		}
		return v.updateList(m)

	case sectionSavedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		v.errMsg = ""
		if m.editIdx == -1 {
			v.items = append(v.items, m.annotation)
			v.cursor = len(v.items) - 1
		} else {
			v.items[m.editIdx] = m.annotation
			v.cursor = m.editIdx
		}
		v.mode = saModeList
		return v, nil

	case sectionDeletedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		v.errMsg = ""
		v.items = append(v.items[:m.idx], v.items[m.idx+1:]...)
		if v.cursor > len(v.items) {
			v.cursor = len(v.items)
		}
		return v, nil
	}

	if v.mode == saModeEdit {
		var cmd tea.Cmd
		v.ta, cmd = v.ta.Update(msg)
		return v, cmd
	}
	return v, nil
}

func (v *SectionAnnotateView) updateList(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "j", "down":
		if v.cursor < len(v.items) {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "n":
		v.openEdit(-1, "")
	case "enter":
		if v.cursor == len(v.items) {
			v.openEdit(-1, "")
		} else {
			v.openEdit(v.cursor, v.items[v.cursor].Content)
		}
	case "d":
		if v.cursor < len(v.items) {
			idx := v.cursor
			id := v.items[idx].ID
			v.loading = true
			return v, func() tea.Msg {
				err := v.c.DeleteAnnotation(id)
				return sectionDeletedMsg{idx: idx, err: err}
			}
		}
	case "esc":
		return v, func() tea.Msg { return msgs.PopViewMsg{} }
	}
	return v, nil
}

func (v *SectionAnnotateView) openEdit(idx int, content string) {
	v.editIdx = idx
	v.ta = newTA()
	if content != "" {
		v.ta.SetValue(content)
	}
	v.mode = saModeEdit
}

func (v *SectionAnnotateView) updateEdit(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "ctrl+s":
		content := strings.TrimSpace(v.ta.Value())
		if content == "" {
			v.errMsg = "annotation cannot be empty"
			return v, nil
		}
		v.loading = true
		v.errMsg = ""
		idx := v.editIdx
		if idx == -1 {
			// New annotation
			tid := v.teamID
			tier := v.tier
			var refPtr *string
			if tier == "item" && v.sectionRef != "" {
				r := v.sectionRef
				refPtr = &r
			}
			return v, func() tea.Msg {
				ann, err := v.c.PostAnnotation(tier, &tid, refPtr, content)
				if err != nil {
					return sectionSavedMsg{editIdx: -1, err: err}
				}
				return sectionSavedMsg{
					annotation: client.SectionAnnotation{ID: ann.ID, Content: ann.Content},
					editIdx:    -1,
				}
			}
		}
		// Edit existing
		id := v.items[idx].ID
		return v, func() tea.Msg {
			err := v.c.PutAnnotation(id, content)
			if err != nil {
				return sectionSavedMsg{editIdx: idx, err: err}
			}
			return sectionSavedMsg{
				annotation: client.SectionAnnotation{ID: id, Content: content},
				editIdx:    idx,
			}
		}
	case "esc":
		v.mode = saModeList
		v.errMsg = ""
		return v, nil
	}
	var cmd tea.Cmd
	v.ta, cmd = v.ta.Update(m)
	return v, cmd
}

func (v *SectionAnnotateView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + selectedStyle.Render(v.name+" annotations") + "\n\n")

	if v.mode == saModeEdit {
		action := "New"
		if v.editIdx >= 0 {
			action = "Edit"
		}
		sb.WriteString("  " + dimStyle.Render(action+" annotation") + "\n\n")
		sb.WriteString(v.ta.View() + "\n")
		if v.errMsg != "" {
			sb.WriteString("\n" + errorStyle.Render("  "+v.errMsg) + "\n")
		}
		if v.loading {
			sb.WriteString("\n  Saving…\n")
		} else {
			sb.WriteString("\n" + dimStyle.Render("  Ctrl+S save  ·  Esc cancel") + "\n")
		}
		return sb.String()
	}

	// List mode
	if len(v.items) == 0 {
		sb.WriteString(dimStyle.Render("  (no annotations yet)") + "\n")
	}
	for i, item := range v.items {
		bullet := "  • "
		content := item.Content
		if len(content) > 72 {
			content = content[:69] + "..."
		}
		line := bullet + content
		if i == v.cursor {
			line = "> • " + selectedStyle.Render(fmt.Sprintf("%-72s", content))
		}
		sb.WriteString(line + "\n")
	}

	// [+ Add] entry
	addLine := "  " + dimStyle.Render("[+ Add annotation]")
	if v.cursor == len(v.items) {
		addLine = "> " + selectedStyle.Render("[+ Add annotation]")
	}
	sb.WriteString(addLine + "\n")

	if v.errMsg != "" {
		sb.WriteString("\n" + errorStyle.Render("  "+v.errMsg) + "\n")
	}
	if v.loading {
		sb.WriteString("\n  Working…\n")
	} else {
		sb.WriteString("\n" + dimStyle.Render("  j/k move  ·  Enter edit  ·  n new  ·  d delete  ·  Esc close") + "\n")
	}
	return sb.String()
}

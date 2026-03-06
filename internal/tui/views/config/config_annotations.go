package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// ---- internal message types ----

type annotationsLoadedMsg struct {
	data *client.GroupedAnnotationsResponse
	err  error
}

type annotationMutatedMsg struct{ err error }

// ---- mode enum ----

type configAnnotationsMode int

const (
	cfgAnnModeNormal configAnnotationsMode = iota
	cfgAnnModeEdit
	cfgAnnModeConfirmDelete
)

// annotationEntry is a flat row in the combined list.
type annotationEntry struct {
	ann    client.AnnotationResponse
	isTeam bool
}

// ConfigAnnotationsView shows all annotations grouped by tier.
type ConfigAnnotationsView struct {
	c       *client.Client
	entries []annotationEntry
	loading bool
	errMsg  string
	cursor  int
	mode    configAnnotationsMode
	editor  textarea.Model
	confirm string
}

// NewConfigAnnotationsView creates a ConfigAnnotationsView.
func NewConfigAnnotationsView(c *client.Client) *ConfigAnnotationsView {
	ta := textarea.New()
	ta.SetWidth(60)
	ta.SetHeight(4)
	return &ConfigAnnotationsView{c: c, loading: true, editor: ta}
}

// Init implements tea.Model.
func (v *ConfigAnnotationsView) Init() tea.Cmd {
	return v.loadAnnotations()
}

func (v *ConfigAnnotationsView) loadAnnotations() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetConfigAnnotations()
		return annotationsLoadedMsg{data: data, err: err}
	}
}

func (v *ConfigAnnotationsView) buildEntries(data *client.GroupedAnnotationsResponse) {
	v.entries = nil
	for _, a := range data.Team {
		v.entries = append(v.entries, annotationEntry{ann: a, isTeam: true})
	}
	for _, a := range data.Item {
		v.entries = append(v.entries, annotationEntry{ann: a, isTeam: false})
	}
}

func (v *ConfigAnnotationsView) currentEntry() *annotationEntry {
	if v.cursor < 0 || v.cursor >= len(v.entries) {
		return nil
	}
	return &v.entries[v.cursor]
}

// Update implements tea.Model.
func (v *ConfigAnnotationsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case annotationsLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else if m.data != nil {
			v.buildEntries(m.data)
		}
		return v, nil

	case annotationMutatedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		}
		v.loading = true
		return v, v.loadAnnotations()

	case tea.KeyMsg:
		switch v.mode {
		case cfgAnnModeEdit:
			return v.handleEditKey(m.String(), msg)
		case cfgAnnModeConfirmDelete:
			return v.handleConfirmKey(m.String())
		default:
			return v.handleNormalKey(m.String())
		}
	}
	return v, nil
}

func (v *ConfigAnnotationsView) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if v.cursor < len(v.entries)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "e":
		if e := v.currentEntry(); e != nil {
			v.editor.SetValue(e.ann.Content)
			v.editor.Focus()
			v.mode = cfgAnnModeEdit
			return v, textarea.Blink
		}
	case "d":
		if e := v.currentEntry(); e != nil {
			v.confirm = fmt.Sprintf("Delete annotation #%d? [y/N]", e.ann.ID)
			v.mode = cfgAnnModeConfirmDelete
		}
	}
	return v, nil
}

func (v *ConfigAnnotationsView) handleEditKey(key string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		v.mode = cfgAnnModeNormal
		return v, nil
	case "ctrl+s":
		e := v.currentEntry()
		if e == nil {
			v.mode = cfgAnnModeNormal
			return v, nil
		}
		content := strings.TrimSpace(v.editor.Value())
		id := e.ann.ID
		c := v.c
		v.mode = cfgAnnModeNormal
		return v, func() tea.Msg {
			err := c.PutAnnotation(id, content)
			return annotationMutatedMsg{err: err}
		}
	}
	var cmd tea.Cmd
	v.editor, cmd = v.editor.Update(msg)
	return v, cmd
}

func (v *ConfigAnnotationsView) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	v.mode = cfgAnnModeNormal
	v.confirm = ""
	if key == "y" || key == "Y" {
		e := v.currentEntry()
		if e == nil {
			return v, nil
		}
		id := e.ann.ID
		c := v.c
		return v, func() tea.Msg {
			err := c.DeleteAnnotation(id)
			return annotationMutatedMsg{err: err}
		}
	}
	return v, nil
}

// View implements tea.Model.
func (v *ConfigAnnotationsView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config — Annotations") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}

	if v.mode == cfgAnnModeEdit {
		sb.WriteString("  Edit content (Ctrl+S to save, Esc to cancel):\n")
		sb.WriteString(v.editor.View() + "\n")
		return sb.String()
	}

	if v.mode == cfgAnnModeConfirmDelete {
		sb.WriteString("  " + v.confirm + "\n")
		return sb.String()
	}

	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}
	if len(v.entries) == 0 {
		sb.WriteString("  No annotations.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	teamHeaderPrinted := false
	itemHeaderPrinted := false

	for i, e := range v.entries {
		if e.isTeam && !teamHeaderPrinted {
			sb.WriteString(cfgSelectedStyle.Render("  Team-level") + "\n")
			teamHeaderPrinted = true
		}
		if !e.isTeam && !itemHeaderPrinted {
			if teamHeaderPrinted {
				sb.WriteString("\n")
			}
			sb.WriteString(cfgSelectedStyle.Render("  Item-level") + "\n")
			itemHeaderPrinted = true
		}

		prefix := "    "
		content := e.ann.Content
		archLabel := ""
		if e.ann.Archived {
			archLabel = cfgDimStyle.Render(" [Archived]")
			content = cfgDimStyle.Render(content)
		}
		if i == v.cursor {
			prefix = "  > "
			if !e.ann.Archived {
				content = cfgSelectedStyle.Render(content)
			}
		}

		ref := ""
		if e.ann.ItemRef != nil {
			ref = cfgDimStyle.Render("  (" + *e.ann.ItemRef + ")")
		}

		sb.WriteString(fmt.Sprintf("%s%s%s%s\n", prefix, content, archLabel, ref))
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *ConfigAnnotationsView) footer() string {
	return "\n" + cfgDimStyle.Render("  j/k navigate  ·  e edit  ·  d delete  ·  Esc back") + "\n"
}

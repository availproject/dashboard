package config

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- message types ----

type slotSavedMsg struct{ err error }

// slotCompatibleTypes maps purpose → acceptable source types for the picker.
var slotCompatibleTypes = map[string][]string{
	"goals_doc":          {"notion_page", "notion_db", "github_file"},
	"task_label":         {"github_label"},
	"marketing_calendar": {"notion_db"},
}

// ConfigSingleSlotView manages a single-item slot (goals_doc, task_label).
type ConfigSingleSlotView struct {
	c        *client.Client
	teamID   int64
	teamName string
	purpose  string
	label    string
	items    []client.TeamConfigSlotItem
	saving   bool
	errMsg   string
}

// NewConfigSingleSlotView creates a ConfigSingleSlotView.
func NewConfigSingleSlotView(c *client.Client, teamID int64, teamName, purpose string, items []client.TeamConfigSlotItem) *ConfigSingleSlotView {
	return &ConfigSingleSlotView{
		c:        c,
		teamID:   teamID,
		teamName: teamName,
		purpose:  purpose,
		label:    slotLabels[purpose],
		items:    items,
	}
}

// Init implements tea.Model.
func (v *ConfigSingleSlotView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *ConfigSingleSlotView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case msgs.SourcePickedMsg:
		v.saving = true
		return v, v.saveSlot(m.Item)

	case slotSavedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return msgs.PopViewMsg{} }

	case tea.KeyMsg:
		switch m.String() {
		case "enter":
			return v, v.pushPicker()
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
	}
	return v, nil
}

func (v *ConfigSingleSlotView) pushPicker() tea.Cmd {
	compatTypes := slotCompatibleTypes[v.purpose]
	picker := NewConfigSourcePickerView(v.c, compatTypes)
	return func() tea.Msg { return msgs.PushViewMsg{View: picker} }
}

func (v *ConfigSingleSlotView) saveSlot(item client.SourceItemResponse) tea.Cmd {
	c := v.c
	purpose := v.purpose
	teamID := v.teamID
	return func() tea.Msg {
		err := c.PutConfigSource(item.ID, "configured", &teamID, purpose, "")
		return slotSavedMsg{err: err}
	}
}

// View implements tea.Model.
func (v *ConfigSingleSlotView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — "+v.teamName+" — "+v.label) + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}
	if v.saving {
		sb.WriteString("  Saving…\n")
		return sb.String()
	}

	sb.WriteString("  Source:\n\n")
	if len(v.items) == 0 {
		sb.WriteString("    (none)\n\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to pick a source  ·  Esc back") + "\n")
	} else {
		item := v.items[0]
		aiLabel := ""
		if item.Provenance == "ai_extracted" {
			aiLabel = "  " + cfgDimStyle.Render("[ai]")
		}
		sb.WriteString("    " + cfgSelectedStyle.Render(item.Title) + aiLabel + "\n")
		sb.WriteString("    " + cfgDimStyle.Render(item.SourceType))
		if item.URL != nil {
			sb.WriteString("  ·  " + cfgDimStyle.Render(*item.URL))
		}
		sb.WriteString("\n\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to replace  ·  Esc back") + "\n")
	}
	return sb.String()
}

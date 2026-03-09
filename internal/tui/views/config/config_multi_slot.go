package config

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- message types ----

type multiSlotLoadedMsg struct {
	items []client.TeamConfigSlotItem
	err   error
}

type itemRemovedMsg struct{ err error }

// slotMultiCompatibleTypes maps purpose → acceptable source types.
var slotMultiCompatibleTypes = map[string][]string{
	"sprint_doc":    {"notion_page", "notion_db", "github_file"},
	"github_repo":   {"github_repo"},
	"metrics_panel": {"notion_page", "notion_db"},
}

// ConfigMultiSlotView manages a multi-item slot (sprint_doc, github_repo, metrics_panel).
type ConfigMultiSlotView struct {
	c        *client.Client
	teamID   int64
	teamName string
	purpose  string
	label    string
	items    []client.TeamConfigSlotItem
	cursor   int
	errMsg   string
}

// NewConfigMultiSlotView creates a ConfigMultiSlotView.
func NewConfigMultiSlotView(c *client.Client, teamID int64, teamName, purpose string, items []client.TeamConfigSlotItem) *ConfigMultiSlotView {
	return &ConfigMultiSlotView{
		c:        c,
		teamID:   teamID,
		teamName: teamName,
		purpose:  purpose,
		label:    slotLabels[purpose],
		items:    items,
	}
}

// Init implements tea.Model.
func (v *ConfigMultiSlotView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *ConfigMultiSlotView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case msgs.SourcePickedMsg:
		return v, v.addItem(m.Item)

	case msgs.DiscoveredItemsSelectedMsg:
		var cmds []tea.Cmd
		for _, item := range m.Items {
			item := item
			cmds = append(cmds, v.addItem(item))
		}
		if len(cmds) > 0 {
			return v, tea.Batch(cmds...)
		}
		return v, nil

	case multiSlotLoadedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.errMsg = ""
			v.items = m.items
			if v.cursor >= len(v.items) && v.cursor > 0 {
				v.cursor = len(v.items) - 1
			}
		}
		return v, nil

	case itemRemovedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.errMsg = ""
			// Reload from server would be ideal; for now just remove from local list.
			if v.cursor < len(v.items) {
				v.items = append(v.items[:v.cursor], v.items[v.cursor+1:]...)
				if v.cursor > 0 && v.cursor >= len(v.items) {
					v.cursor--
				}
			}
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.cursor < len(v.items)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "a":
			return v, v.pushPicker()
		case "x":
			return v, v.removeItem()
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
	}
	return v, nil
}

func (v *ConfigMultiSlotView) pushPicker() tea.Cmd {
	compatTypes := slotMultiCompatibleTypes[v.purpose]
	picker := NewConfigSourcePickerView(v.c, compatTypes)
	return func() tea.Msg { return msgs.PushViewMsg{View: picker} }
}

func (v *ConfigMultiSlotView) addItem(item client.SourceItemResponse) tea.Cmd {
	c := v.c
	purpose := v.purpose
	teamID := v.teamID
	return func() tea.Msg {
		if err := c.PutConfigSource(item.ID, "configured", &teamID, purpose, ""); err != nil {
			return multiSlotLoadedMsg{err: err}
		}
		cfg, err := c.GetTeamConfig(teamID)
		if err != nil {
			return multiSlotLoadedMsg{err: err}
		}
		return multiSlotLoadedMsg{items: cfg.Slots[purpose]}
	}
}

func (v *ConfigMultiSlotView) removeItem() tea.Cmd {
	if len(v.items) == 0 || v.cursor >= len(v.items) {
		return nil
	}
	item := v.items[v.cursor]
	c := v.c
	configID := item.ID
	catalogueID := item.CatalogueID
	return func() tea.Msg {
		err := c.DeleteSourceConfig(catalogueID, configID)
		return itemRemovedMsg{err: err}
	}
}

// View implements tea.Model.
func (v *ConfigMultiSlotView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — "+v.teamName+" — "+v.label) + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}

	// Group by provenance.
	var aiItems, manualItems []client.TeamConfigSlotItem
	for _, it := range v.items {
		if it.Provenance == "ai_extracted" {
			aiItems = append(aiItems, it)
		} else {
			manualItems = append(manualItems, it)
		}
	}

	// AI section.
	sb.WriteString("  " + cfgDimStyle.Render("From homepage (AI):") + "\n")
	if len(aiItems) == 0 {
		sb.WriteString("    " + cfgDimStyle.Render("(none found)") + "\n")
	} else {
		aiStart := 0
		for i, it := range aiItems {
			globalIdx := aiStart + i
			line := v.renderItem(it, globalIdx)
			sb.WriteString(line + "\n")
		}
	}

	// Manual section.
	sb.WriteString("\n  " + cfgDimStyle.Render("Manually added:") + "\n")
	if len(manualItems) == 0 {
		sb.WriteString("    " + cfgDimStyle.Render("(none)") + "\n")
	} else {
		manualStart := len(aiItems)
		for i, it := range manualItems {
			globalIdx := manualStart + i
			line := v.renderItem(it, globalIdx)
			sb.WriteString(line + "\n")
		}
	}

	sb.WriteString("\n" + cfgDimStyle.Render("  a add  ·  x remove  ·  j/k navigate  ·  Esc back") + "\n")
	return sb.String()
}

func (v *ConfigMultiSlotView) renderItem(it client.TeamConfigSlotItem, idx int) string {
	prefix := "    "
	title := truncate(it.Title, 50)
	badge := ""
	if it.SprintStatus != nil {
		badge = "  " + cfgDimStyle.Render("["+*it.SprintStatus+"]")
	}
	if idx == v.cursor {
		prefix = "  > "
		return prefix + cfgSelectedStyle.Render(fmt.Sprintf("%-50s", title)) + badge
	}
	return prefix + title + badge
}

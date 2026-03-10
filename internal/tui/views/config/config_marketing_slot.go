package config

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- message types ----

type marketingLabelsLoadedMsg struct {
	labels []string
	err    error
}

type marketingLabelSavedMsg struct{ err error }

type marketingDBSavedMsg struct{ err error }

// marketingSlotMode describes what the view is currently doing.
type marketingSlotMode int

const (
	marketingSlotModeNormal marketingSlotMode = iota
	marketingSlotModePickingLabel
)

// ConfigMarketingSlotView manages the marketing_calendar slot.
// It shows two configurable rows: the Notion DB and the project label.
type ConfigMarketingSlotView struct {
	c              *client.Client
	teamID         int64
	teamName       string
	dbItems        []client.TeamConfigSlotItem // current marketing_calendar source configs
	currentLabel   *string                     // current marketing_label from team
	cursor         int                         // 0 = DB row, 1 = label row
	mode           marketingSlotMode
	labels         []string // fetched label options
	labelCursor    int
	labelScroll    int
	height         int
	loadingLabels  bool
	errMsg         string
	saving         bool
}

// NewConfigMarketingSlotView creates a ConfigMarketingSlotView.
func NewConfigMarketingSlotView(c *client.Client, teamID int64, teamName string, dbItems []client.TeamConfigSlotItem, currentLabel *string) *ConfigMarketingSlotView {
	return &ConfigMarketingSlotView{
		c:            c,
		teamID:       teamID,
		teamName:     teamName,
		dbItems:      dbItems,
		currentLabel: currentLabel,
	}
}

// Init implements tea.Model.
func (v *ConfigMarketingSlotView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *ConfigMarketingSlotView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case msgs.SourcePickedMsg:
		// DB was selected from the picker.
		v.saving = true
		return v, v.saveDB(m.Item)

	case marketingDBSavedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return msgs.PopViewMsg{} }

	case marketingLabelsLoadedMsg:
		v.loadingLabels = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		sort.Strings(m.labels)
		v.labels = m.labels
		v.labelCursor = 0
		// Pre-select current label if set.
		if v.currentLabel != nil {
			for i, l := range v.labels {
				if l == *v.currentLabel {
					v.labelCursor = i
					break
				}
			}
		}
		v.mode = marketingSlotModePickingLabel
		return v, nil

	case marketingLabelSavedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return msgs.PopViewMsg{} }

	case tea.KeyMsg:
		if v.mode == marketingSlotModePickingLabel {
			return v.handleLabelPickKey(m.String())
		}
		return v.handleNormalKey(m.String())

	case tea.WindowSizeMsg:
		v.height = m.Height
	}
	return v, nil
}

func (v *ConfigMarketingSlotView) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if v.cursor < 1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		if v.cursor == 0 {
			return v, v.pushDBPicker()
		}
		if v.cursor == 1 {
			return v, v.fetchLabels()
		}
	case "esc":
		return v, func() tea.Msg { return msgs.PopViewMsg{} }
	}
	return v, nil
}

func (v *ConfigMarketingSlotView) handleLabelPickKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if v.labelCursor < len(v.labels)-1 {
			v.labelCursor++
			v.clampLabelScroll()
		}
	case "k", "up":
		if v.labelCursor > 0 {
			v.labelCursor--
			v.clampLabelScroll()
		}
	case "enter":
		if v.labelCursor < len(v.labels) {
			return v, v.saveLabel(v.labels[v.labelCursor])
		}
	case "esc":
		v.mode = marketingSlotModeNormal
	}
	return v, nil
}

func (v *ConfigMarketingSlotView) labelVisibleLines() int {
	if v.height <= 0 {
		return 15
	}
	return max(5, v.height-10)
}

func (v *ConfigMarketingSlotView) clampLabelScroll() {
	avail := v.labelVisibleLines()
	if v.labelCursor < v.labelScroll {
		v.labelScroll = v.labelCursor
	}
	if v.labelCursor >= v.labelScroll+avail {
		v.labelScroll = v.labelCursor - avail + 1
	}
}

func (v *ConfigMarketingSlotView) pushDBPicker() tea.Cmd {
	picker := NewConfigSourcePickerView(v.c, []string{"notion_db"})
	return func() tea.Msg { return msgs.PushViewMsg{View: picker} }
}

func (v *ConfigMarketingSlotView) fetchLabels() tea.Cmd {
	if len(v.dbItems) == 0 {
		v.errMsg = "configure the marketing calendar DB first"
		return nil
	}
	v.loadingLabels = true
	v.errMsg = ""
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		labels, err := c.GetTeamMarketingLabels(teamID)
		return marketingLabelsLoadedMsg{labels: labels, err: err}
	}
}

func (v *ConfigMarketingSlotView) saveDB(item client.SourceItemResponse) tea.Cmd {
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		err := c.PutConfigSource(item.ID, "configured", &teamID, "marketing_calendar", "")
		return marketingDBSavedMsg{err: err}
	}
}

func (v *ConfigMarketingSlotView) saveLabel(label string) tea.Cmd {
	v.saving = true
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		err := c.PutTeamMarketingLabel(teamID, label)
		return marketingLabelSavedMsg{err: err}
	}
}

// View implements tea.Model.
func (v *ConfigMarketingSlotView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — "+v.teamName+" — Marketing Calendar") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}
	if v.saving {
		sb.WriteString("  Saving…\n")
		return sb.String()
	}
	if v.loadingLabels {
		sb.WriteString("  Loading labels…\n")
		return sb.String()
	}

	if v.mode == marketingSlotModePickingLabel {
		avail := v.labelVisibleLines()
		start := v.labelScroll
		end := start + avail
		if end > len(v.labels) {
			end = len(v.labels)
		}
		sb.WriteString(fmt.Sprintf("  Select project label: (%d/%d)\n\n", v.labelCursor+1, len(v.labels)))
		for i := start; i < end; i++ {
			label := v.labels[i]
			prefix := "    "
			line := label
			if i == v.labelCursor {
				prefix = "  > "
				line = cfgSelectedStyle.Render(label)
			}
			sb.WriteString(prefix + line + "\n")
		}
		sb.WriteString("\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter select  ·  Esc cancel") + "\n")
		return sb.String()
	}

	// Row 0: DB
	rows := []struct {
		label   string
		summary string
	}{
		{"Notion DB", v.dbSummary()},
		{"Project label", v.labelSummary()},
	}
	for i, row := range rows {
		var line string
		if i == v.cursor {
			line = "> " + cfgSelectedStyle.Render(fmt.Sprintf("%-18s  %s", row.label, row.summary))
		} else {
			line = fmt.Sprintf("  %-18s  %s", row.label, row.summary)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter edit  ·  Esc back") + "\n")
	return sb.String()
}

func (v *ConfigMarketingSlotView) dbSummary() string {
	if len(v.dbItems) == 0 {
		return cfgDimStyle.Render("(none — start here)")
	}
	return truncate(v.dbItems[0].Title, 45)
}

func (v *ConfigMarketingSlotView) labelSummary() string {
	if len(v.dbItems) == 0 {
		return cfgDimStyle.Render("(set DB first)")
	}
	if v.currentLabel == nil || *v.currentLabel == "" {
		return cfgDimStyle.Render("(none)")
	}
	return *v.currentLabel
}

package config

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- message types ----

type boardFieldsLoadedMsg struct {
	fields []client.ProjectField
	err    error
}

type boardSlotSavedMsg struct{ err error }

type boardSwitchedMsg struct{ err error }

// boardSlotMode describes the current picker state.
type boardSlotMode int

const (
	boardSlotModeOverview    boardSlotMode = iota
	boardSlotModePickingField // pick a field name (for team_area_field or sprint_field)
	boardSlotModePickingValue // pick a value (for team_area_value)
)

// boardSlotRow identifies which setting row is focused.
type boardSlotRow int

const (
	boardRowBoard boardSlotRow = iota
	boardRowTeamAreaField
	boardRowTeamAreaValue
	boardRowSprintField
	boardRowCount
)

// ConfigBoardSlotView edits the filter configuration for a github_project slot.
// It shows the linked board source (selectable to override) and lets the user pick:
//   - Team/Area field name from a list
//   - Team/Area value from a list (options of the selected field)
//   - Sprint field name from a list
type ConfigBoardSlotView struct {
	c        *client.Client
	teamID   int64
	teamName string
	items    []client.TeamConfigSlotItem

	// current (pending) selections
	teamAreaField string
	teamAreaValue string
	sprintField   string

	// loaded data
	fields  []client.ProjectField
	loading bool
	loadErr string

	// overview state
	cursor int
	mode   boardSlotMode

	// picker state (field or value list)
	editingRow boardSlotRow
	pickList   []string
	pickCursor int
	pickScroll int

	height  int
	saving  bool
	errMsg  string
}

// NewConfigBoardSlotView creates a ConfigBoardSlotView pre-populated from
// the first item's BoardConfig (if present).
func NewConfigBoardSlotView(c *client.Client, teamID int64, teamName string, items []client.TeamConfigSlotItem) *ConfigBoardSlotView {
	v := &ConfigBoardSlotView{
		c:        c,
		teamID:   teamID,
		teamName: teamName,
		items:    items,
		loading:  len(items) > 0,
	}
	if len(items) > 0 && items[0].BoardConfig != nil {
		bc := items[0].BoardConfig
		v.teamAreaField = bc.TeamAreaField
		v.teamAreaValue = bc.TeamAreaValue
		v.sprintField = bc.SprintField
	}
	return v
}

// Init implements tea.Model.
func (v *ConfigBoardSlotView) Init() tea.Cmd {
	if len(v.items) == 0 {
		return nil
	}
	return v.loadFields()
}

func (v *ConfigBoardSlotView) loadFields() tea.Cmd {
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		fields, err := c.GetBoardFields(teamID)
		return boardFieldsLoadedMsg{fields: fields, err: err}
	}
}

// Update implements tea.Model.
func (v *ConfigBoardSlotView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case boardFieldsLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.loadErr = m.err.Error()
			return v, nil
		}
		v.fields = m.fields
		return v, nil

	case msgs.SourcePickedMsg:
		// A new board was selected from the picker.
		return v, v.switchBoard(m.Item)

	case boardSwitchedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return msgs.PopViewMsg{} }

	case boardSlotSavedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return msgs.PopViewMsg{} }

	case tea.WindowSizeMsg:
		v.height = m.Height
		return v, nil

	case tea.KeyMsg:
		switch v.mode {
		case boardSlotModeOverview:
			return v.handleOverviewKey(m.String())
		case boardSlotModePickingField, boardSlotModePickingValue:
			return v.handlePickerKey(m.String())
		}
	}
	return v, nil
}

func (v *ConfigBoardSlotView) handleOverviewKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		return v, func() tea.Msg { return msgs.PopViewMsg{} }
	case "j", "down", "tab":
		if v.cursor < int(boardRowCount)-1 {
			v.cursor++
		}
	case "k", "up", "shift+tab":
		if v.cursor > 0 {
			v.cursor--
		}
	case "enter":
		if v.cursor == int(boardRowBoard) {
			// Open source picker to select/override the board.
			picker := NewConfigSourcePickerView(v.c, []string{"github_project"})
			return v, func() tea.Msg { return msgs.PushViewMsg{View: picker} }
		}
		if len(v.items) == 0 || v.loading {
			return v, nil
		}
		return v.openPicker(boardSlotRow(v.cursor))
	case "ctrl+s":
		return v, v.save()
	}
	return v, nil
}

func (v *ConfigBoardSlotView) openPicker(row boardSlotRow) (tea.Model, tea.Cmd) {
	v.editingRow = row
	v.pickCursor = 0
	v.pickScroll = 0
	v.errMsg = ""

	switch row {
	case boardRowTeamAreaField:
		v.pickList = v.fieldNames()
		v.pickCursor = v.indexIn(v.pickList, v.teamAreaField)
		v.mode = boardSlotModePickingField

	case boardRowTeamAreaValue:
		v.pickList = v.optionsForField(v.teamAreaField)
		v.pickCursor = v.indexIn(v.pickList, v.teamAreaValue)
		if len(v.pickList) == 0 {
			v.errMsg = "select a Team/Area field first"
			return v, nil
		}
		v.mode = boardSlotModePickingValue

	case boardRowSprintField:
		v.pickList = v.fieldNames()
		v.pickCursor = v.indexIn(v.pickList, v.sprintField)
		v.mode = boardSlotModePickingField
	}

	v.clampPickScroll()
	return v, nil
}

func (v *ConfigBoardSlotView) handlePickerKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		v.mode = boardSlotModeOverview
	case "j", "down":
		if v.pickCursor < len(v.pickList)-1 {
			v.pickCursor++
			v.clampPickScroll()
		}
	case "k", "up":
		if v.pickCursor > 0 {
			v.pickCursor--
			v.clampPickScroll()
		}
	case "enter":
		if v.pickCursor < len(v.pickList) {
			v.applyPick(v.pickList[v.pickCursor])
		}
		v.mode = boardSlotModeOverview
	}
	return v, nil
}

func (v *ConfigBoardSlotView) applyPick(val string) {
	switch v.editingRow {
	case boardRowTeamAreaField:
		if v.teamAreaField != val {
			v.teamAreaValue = ""
		}
		v.teamAreaField = val
	case boardRowTeamAreaValue:
		v.teamAreaValue = val
	case boardRowSprintField:
		v.sprintField = val
	}
}

// switchBoard updates the source config to point to a different catalogue item.
// It deletes the old source_config (if any) and creates a new one, preserving
// the current filter settings (teamAreaField, teamAreaValue, sprintField).
func (v *ConfigBoardSlotView) switchBoard(newItem client.SourceItemResponse) tea.Cmd {
	v.saving = true
	v.errMsg = ""
	bc := client.BoardConfigMeta{
		TeamAreaField: strings.TrimSpace(v.teamAreaField),
		TeamAreaValue: strings.TrimSpace(v.teamAreaValue),
		SprintField:   strings.TrimSpace(v.sprintField),
	}
	metaJSON, _ := json.Marshal(bc)
	c := v.c
	teamID := v.teamID
	oldItems := v.items
	return func() tea.Msg {
		// Delete existing source_config(s) so we don't accumulate duplicates.
		for _, old := range oldItems {
			_ = c.DeleteSourceConfig(old.CatalogueID, old.ID)
		}
		// Create new source_config pointing to the newly picked board.
		err := c.PutConfigSource(newItem.ID, "configured", &teamID, "github_project", string(metaJSON))
		return boardSwitchedMsg{err: err}
	}
}

func (v *ConfigBoardSlotView) save() tea.Cmd {
	if len(v.items) == 0 || v.saving {
		return nil
	}
	v.saving = true
	v.errMsg = ""
	item := v.items[0]
	bc := client.BoardConfigMeta{
		TeamAreaField: strings.TrimSpace(v.teamAreaField),
		TeamAreaValue: strings.TrimSpace(v.teamAreaValue),
		SprintField:   strings.TrimSpace(v.sprintField),
	}
	metaJSON, _ := json.Marshal(bc)
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		err := c.PutConfigSource(item.CatalogueID, "configured", &teamID, "github_project", string(metaJSON))
		return boardSlotSavedMsg{err: err}
	}
}

// ---- helpers ----

func (v *ConfigBoardSlotView) fieldNames() []string {
	names := make([]string, 0, len(v.fields))
	for _, f := range v.fields {
		names = append(names, f.Name)
	}
	return names
}

func (v *ConfigBoardSlotView) optionsForField(fieldName string) []string {
	for _, f := range v.fields {
		if f.Name == fieldName {
			return f.Options
		}
	}
	return nil
}

func (v *ConfigBoardSlotView) indexIn(list []string, val string) int {
	for i, s := range list {
		if s == val {
			return i
		}
	}
	return 0
}

func (v *ConfigBoardSlotView) visibleLines() int {
	if v.height <= 0 {
		return 15
	}
	return max(5, v.height-12)
}

func (v *ConfigBoardSlotView) clampPickScroll() {
	avail := v.visibleLines()
	if v.pickCursor < v.pickScroll {
		v.pickScroll = v.pickCursor
	}
	if v.pickCursor >= v.pickScroll+avail {
		v.pickScroll = v.pickCursor - avail + 1
	}
}

// ---- View ----

// View implements tea.Model.
func (v *ConfigBoardSlotView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — "+v.teamName+" — Project Board") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}

	if v.saving {
		sb.WriteString("  Saving…\n")
		return sb.String()
	}

	if v.mode == boardSlotModePickingField || v.mode == boardSlotModePickingValue {
		return v.viewPicker(&sb)
	}

	return v.viewOverview(&sb)
}

func (v *ConfigBoardSlotView) viewOverview(sb *strings.Builder) string {
	// Row 0: Board (always shown, clickable to override).
	boardTitle := cfgDimStyle.Render("(not set — configure homepage or press Enter to pick)")
	boardURL := ""
	if len(v.items) > 0 {
		boardTitle = v.items[0].Title
		if v.items[0].URL != nil {
			boardURL = *v.items[0].URL
		}
	}

	rows := []struct {
		label string
		value string
		sub   string // optional sub-line (URL)
	}{
		{"Board", boardTitle, boardURL},
		{"Team/Area field", v.teamAreaField, ""},
		{"Team/Area value", v.teamAreaValue, ""},
		{"Sprint field", v.sprintField, ""},
	}

	for i, row := range rows {
		val := row.value
		if val == "" && i > 0 {
			val = cfgDimStyle.Render("(none)")
		}

		var line string
		if i == v.cursor {
			if row.value == "" && i > 0 {
				line = "> " + cfgSelectedStyle.Render(fmt.Sprintf("%-18s", row.label)) + "  " + cfgDimStyle.Render("(none)")
			} else {
				line = "> " + cfgSelectedStyle.Render(fmt.Sprintf("%-18s  %s", row.label, row.value))
			}
		} else {
			line = fmt.Sprintf("  %-18s  %s", row.label, val)
		}
		sb.WriteString(line + "\n")

		if row.sub != "" {
			sb.WriteString(fmt.Sprintf("  %-18s  %s\n", "", cfgDimStyle.Render(row.sub)))
		}
	}

	sb.WriteString("\n")

	hint := "j/k navigate  ·  Enter pick  ·  Ctrl+S save filters  ·  Esc back"
	if len(v.items) == 0 {
		hint = "j/k navigate  ·  Enter pick board  ·  Esc back"
	}
	if v.loading {
		sb.WriteString("  Loading board fields…\n")
	} else if v.loadErr != "" {
		sb.WriteString("  " + cfgDimStyle.Render("(could not load fields: "+v.loadErr+")") + "\n")
	}
	sb.WriteString(cfgDimStyle.Render("  "+hint) + "\n")
	return sb.String()
}

func (v *ConfigBoardSlotView) viewPicker(sb *strings.Builder) string {
	var label string
	switch v.editingRow {
	case boardRowTeamAreaField:
		label = "Select Team/Area field"
	case boardRowTeamAreaValue:
		label = "Select Team/Area value"
	case boardRowSprintField:
		label = "Select Sprint field"
	}

	total := len(v.pickList)
	cur := v.pickCursor + 1
	if total == 0 {
		cur = 0
	}
	sb.WriteString(fmt.Sprintf("  %s: (%d/%d)\n\n", label, cur, total))

	avail := v.visibleLines()
	start := v.pickScroll
	end := start + avail
	if end > total {
		end = total
	}
	for i := start; i < end; i++ {
		item := v.pickList[i]
		if i == v.pickCursor {
			sb.WriteString("  > " + cfgSelectedStyle.Render(item) + "\n")
		} else {
			sb.WriteString("    " + item + "\n")
		}
	}
	sb.WriteString("\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter select  ·  Esc cancel") + "\n")
	return sb.String()
}

package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- internal message types ----

type sourcesLoadedMsg struct {
	items []client.SourceItemResponse
	err   error
}

type teamsForSourcesLoadedMsg struct {
	teams []client.TeamItem
	err   error
}

// discoverCompletedMsg is sent by the discover view when a discovery run
// finishes, so the sources list can auto-reload and display the outcome.
type discoverCompletedMsg struct {
	errMsg string // empty on success
}

type ignoreItemMsg struct {
	err      error
	wasIgnore bool // true = just ignored, false = just unignored
}

type classifySubmittedMsg struct {
	runID int64
	err   error
}

type classifyPollMsg struct{ runID int64 }

type classifyFinishedMsg struct {
	errMsg string // empty on success
}

// ---- status filter ----

var sourceStatusFilters = []string{"all", "untagged", "configured", "ignored"}

// ConfigSourcesView shows the source catalogue with tagging support.
type ConfigSourcesView struct {
	c            *client.Client
	items        []client.SourceItemResponse
	teams        []client.TeamItem
	loading      bool
	errMsg       string
	cursor       int
	scrollOffset int
	height       int // terminal height from WindowSizeMsg
	filterIdx    int // index into sourceStatusFilters
	discoverMsg  string
	// search
	searchMode bool
	search     textinput.Model
	// classify
	classifying bool
	classifyMsg string
}

// NewConfigSourcesView creates a ConfigSourcesView.
func NewConfigSourcesView(c *client.Client) *ConfigSourcesView {
	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Width = 40
	return &ConfigSourcesView{c: c, loading: true, search: ti}
}

// Init implements tea.Model.
func (v *ConfigSourcesView) Init() tea.Cmd {
	return tea.Batch(v.loadItems(), v.loadTeams())
}

func (v *ConfigSourcesView) loadItems() tea.Cmd {
	return func() tea.Msg {
		items, err := v.c.GetConfigSources()
		return sourcesLoadedMsg{items: items, err: err}
	}
}

func (v *ConfigSourcesView) loadTeams() tea.Cmd {
	return func() tea.Msg {
		teams, err := v.c.GetTeams()
		return teamsForSourcesLoadedMsg{teams: teams, err: err}
	}
}

// treeItem is a flattened tree node with its visual prefix pre-computed.
type treeItem struct {
	item   client.SourceItemResponse
	prefix string // e.g. "  ├─ " or "  └─ "
}

func (v *ConfigSourcesView) filtered() []client.SourceItemResponse {
	filter := sourceStatusFilters[v.filterIdx]
	q := strings.ToLower(strings.TrimSpace(v.search.Value()))
	var out []client.SourceItemResponse
	for _, it := range v.items {
		if filter != "all" && it.Status != filter {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Title), q) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// isTreeMode returns true when no filter or search is active.
func (v *ConfigSourcesView) isTreeMode() bool {
	return sourceStatusFilters[v.filterIdx] == "all" &&
		strings.TrimSpace(v.search.Value()) == ""
}

// buildTree converts all items into a DFS-ordered flat list with tree prefixes.
func buildTree(items []client.SourceItemResponse) []treeItem {
	// Group children by parent ID.
	childrenOf := make(map[int64][]client.SourceItemResponse)
	var roots []client.SourceItemResponse
	for _, it := range items {
		if it.ParentID == nil {
			roots = append(roots, it)
		} else {
			childrenOf[*it.ParentID] = append(childrenOf[*it.ParentID], it)
		}
	}

	var result []treeItem
	var walk func(item client.SourceItemResponse, connector, childIndent string)
	walk = func(item client.SourceItemResponse, connector, childIndent string) {
		result = append(result, treeItem{item: item, prefix: connector})
		kids := childrenOf[item.ID]
		for i, kid := range kids {
			if i < len(kids)-1 {
				walk(kid, childIndent+"├─ ", childIndent+"│  ")
			} else {
				walk(kid, childIndent+"└─ ", childIndent+"   ")
			}
		}
	}

	for _, r := range roots {
		walk(r, "", "  ")
	}
	return result
}

// Update implements tea.Model.
func (v *ConfigSourcesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		v.height = m.Height
		return v, nil

	case sourcesLoadedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.items = m.items
		}
		if v.teams != nil {
			v.loading = false
		}
		return v, nil

	case teamsForSourcesLoadedMsg:
		if m.err == nil {
			v.teams = m.teams
		}
		if v.items != nil {
			v.loading = false
		}
		return v, nil

	case discoverCompletedMsg:
		if m.errMsg != "" {
			v.discoverMsg = "Discovery failed: " + m.errMsg
		} else {
			v.discoverMsg = "Discovery complete."
		}
		v.loading = true
		v.cursor = 0
		return v, v.loadItems()

	case ignoreItemMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			if v.cursor < v.displayLen()-1 {
				v.cursor++
			}
			v.loading = true
			return v, v.loadItems()
		}
		return v, nil

	case classifySubmittedMsg:
		if m.err != nil {
			v.classifying = false
			v.classifyMsg = "Classify failed: " + m.err.Error()
			return v, nil
		}
		return v, v.pollClassify(m.runID)

	case classifyPollMsg:
		return v, v.pollClassify(m.runID)

	case classifyFinishedMsg:
		v.classifying = false
		if m.errMsg != "" {
			v.classifyMsg = "Classify failed: " + m.errMsg
		} else {
			v.classifyMsg = "Classification complete."
		}
		v.loading = true
		return v, v.loadItems()

	case tea.KeyMsg:
		// Search mode: route most keys to the text input.
		if v.searchMode {
			switch m.String() {
			case "esc":
				v.searchMode = false
				v.search.Blur()
				v.search.SetValue("")
				v.cursor = 0
				return v, nil
			case "enter":
				v.searchMode = false
				v.search.Blur()
				v.cursor = 0
				return v, nil
			default:
				var cmd tea.Cmd
				v.search, cmd = v.search.Update(msg)
				v.cursor = 0
				return v, cmd
			}
		}

		// Normal mode.
		switch m.String() {
		case "j", "down":
			if v.cursor < v.displayLen()-1 {
				v.cursor++
				v.clampScroll()
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				v.clampScroll()
			}
			return v, nil
		case "f":
			v.filterIdx = (v.filterIdx + 1) % len(sourceStatusFilters)
			v.cursor = 0
			return v, nil
		case "/":
			v.searchMode = true
			v.search.Focus()
			return v, textinput.Blink
		case "enter":
			return v, v.pushTagView()
		case "x":
			return v, v.ignoreItem()
		case "X":
			return v, v.ignoreRecursive()
		case "A":
			if !v.classifying {
				return v, v.startClassify()
			}
			return v, nil
		case "D":
			return v, v.pushDiscoverPrompt()
		case "r":
			v.loading = true
			v.cursor = 0
			return v, v.loadItems()
		}
	}
	return v, nil
}

func (v *ConfigSourcesView) currentItem() (client.SourceItemResponse, bool) {
	if v.isTreeMode() {
		tree := buildTree(v.items)
		if v.cursor < 0 || v.cursor >= len(tree) {
			return client.SourceItemResponse{}, false
		}
		return tree[v.cursor].item, true
	}
	filtered := v.filtered()
	if v.cursor < 0 || v.cursor >= len(filtered) {
		return client.SourceItemResponse{}, false
	}
	return filtered[v.cursor], true
}

func (v *ConfigSourcesView) displayLen() int {
	if v.isTreeMode() {
		return len(buildTree(v.items))
	}
	return len(v.filtered())
}

func (v *ConfigSourcesView) pushTagView() tea.Cmd {
	item, ok := v.currentItem()
	if !ok {
		return nil
	}
	tv := newConfigTagView(v.c, item, v.teams)
	return func() tea.Msg { return msgs.PushViewMsg{View: tv} }
}

func (v *ConfigSourcesView) pushDiscoverPrompt() tea.Cmd {
	dv := newConfigDiscoverView(v.c)
	return func() tea.Msg { return msgs.PushViewMsg{View: dv} }
}

func (v *ConfigSourcesView) availableItemLines() int {
	if v.height <= 0 {
		return 20 // sensible default before first WindowSizeMsg
	}
	return max(5, v.height-9) // 9 = header (5) + footer (3) + margin (1)
}

func (v *ConfigSourcesView) clampScroll() {
	avail := v.availableItemLines()
	if v.cursor < v.scrollOffset {
		v.scrollOffset = v.cursor
	}
	if v.cursor >= v.scrollOffset+avail {
		v.scrollOffset = v.cursor - avail + 1
	}
}

// descendantIDs returns the IDs of all descendants of the item with the given ID.
func descendantIDs(id int64, items []client.SourceItemResponse) []int64 {
	var result []int64
	for _, it := range items {
		if it.ParentID != nil && *it.ParentID == id {
			result = append(result, it.ID)
			result = append(result, descendantIDs(it.ID, items)...)
		}
	}
	return result
}

func (v *ConfigSourcesView) ignoreItem() tea.Cmd {
	item, ok := v.currentItem()
	if !ok {
		return nil
	}
	c := v.c
	if item.Status == "ignored" {
		return func() tea.Msg {
			err := c.PutConfigSource(item.ID, "untagged", nil, "", "")
			return ignoreItemMsg{err: err, wasIgnore: false}
		}
	}
	return func() tea.Msg {
		err := c.PutConfigSource(item.ID, "ignored", nil, "", "")
		return ignoreItemMsg{err: err, wasIgnore: true}
	}
}

func (v *ConfigSourcesView) ignoreRecursive() tea.Cmd {
	item, ok := v.currentItem()
	if !ok {
		return nil
	}
	ids := append([]int64{item.ID}, descendantIDs(item.ID, v.items)...)
	c := v.c
	return func() tea.Msg {
		for _, id := range ids {
			if err := c.PutConfigSource(id, "ignored", nil, "", ""); err != nil {
				return ignoreItemMsg{err: err, wasIgnore: true}
			}
		}
		return ignoreItemMsg{wasIgnore: true}
	}
}

func (v *ConfigSourcesView) startClassify() tea.Cmd {
	filtered := v.filtered()
	var ids []int64
	for _, it := range filtered {
		if it.Status == "ignored" {
			continue
		}
		if it.AISuggestedPurpose == nil {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	v.classifying = true
	v.classifyMsg = fmt.Sprintf("Classifying %d item(s)…", len(ids))
	c := v.c
	return func() tea.Msg {
		runID, err := c.PostClassify(ids)
		return classifySubmittedMsg{runID: runID, err: err}
	}
}

func (v *ConfigSourcesView) pollClassify(runID int64) tea.Cmd {
	c := v.c
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return classifyFinishedMsg{errMsg: err.Error()}
		}
		switch run.Status {
		case "completed", "done":
			return classifyFinishedMsg{}
		case "failed", "error":
			errMsg := "classify failed"
			if run.Error != nil {
				errMsg = *run.Error
			}
			return classifyFinishedMsg{errMsg: errMsg}
		default:
			return classifyPollMsg{runID: runID}
		}
	}
}

// View implements tea.Model.
func (v *ConfigSourcesView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config — Sources") + "\n\n")

	filter := sourceStatusFilters[v.filterIdx]
	sb.WriteString(cfgDimStyle.Render(fmt.Sprintf("  Filter: [%s]  (f to cycle)", filter)) + "\n")

	if v.searchMode {
		sb.WriteString("  Search: " + v.search.View() + "\n")
	} else if v.search.Value() != "" {
		sb.WriteString(cfgDimStyle.Render("  Search: "+v.search.Value()+"  (/ to edit, Esc to clear)") + "\n")
	}
	sb.WriteString("\n")

	if v.discoverMsg != "" {
		sb.WriteString(cfgDimStyle.Render("  "+v.discoverMsg) + "\n\n")
	}
	if v.classifyMsg != "" {
		sb.WriteString(cfgDimStyle.Render("  "+v.classifyMsg) + "\n\n")
	}
	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}
	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	avail := v.availableItemLines()

	if v.isTreeMode() {
		tree := buildTree(v.items)
		if len(tree) == 0 {
			sb.WriteString("  No items. Press D to run discovery.\n")
			sb.WriteString(v.footer())
			return sb.String()
		}
		start := v.scrollOffset
		end := min(start+avail, len(tree))
		for i := start; i < end; i++ {
			node := tree[i]
			item := node.item
			typeLabel := "[" + item.SourceType + "]"
			purpose := item.Status
			if len(item.Configs) > 0 {
				purpose = item.Configs[0].Purpose
			}
			ai := ""
			if item.AISuggestedPurpose != nil {
				ai = cfgDimStyle.Render("  ai:" + *item.AISuggestedPurpose)
			}
			linePrefix := "  " + node.prefix
			titleWidth := max(20, 46-len(node.prefix))
			row := fmt.Sprintf("%s%-16s  %-*s  %-10s  %s%s",
				linePrefix, typeLabel, titleWidth, truncate(item.Title, titleWidth), item.Status, purpose, ai)
			if i == v.cursor {
				row = "> " + node.prefix + cfgSelectedStyle.Render(fmt.Sprintf("%-16s  %-*s  %-10s  %s",
					typeLabel, titleWidth, truncate(item.Title, titleWidth), item.Status, purpose))
				if item.AISuggestedPurpose != nil {
					row += cfgDimStyle.Render("  ai:" + *item.AISuggestedPurpose)
				}
			}
			sb.WriteString(row + "\n")
		}
	} else {
		filtered := v.filtered()
		if len(filtered) == 0 {
			sb.WriteString("  No items. Press D to run discovery.\n")
			sb.WriteString(v.footer())
			return sb.String()
		}
		start := v.scrollOffset
		end := min(start+avail, len(filtered))
		for i := start; i < end; i++ {
			item := filtered[i]
			prefix := "  "
			typeLabel := "[" + item.SourceType + "]"
			purpose := item.Status
			if len(item.Configs) > 0 {
				purpose = item.Configs[0].Purpose
			}
			ai := ""
			if item.AISuggestedPurpose != nil {
				ai = cfgDimStyle.Render("  ai:" + *item.AISuggestedPurpose)
			}
			row := fmt.Sprintf("%-16s  %-40s  %-10s  %s%s",
				typeLabel, truncate(item.Title, 40), item.Status, purpose, ai)
			if i == v.cursor {
				prefix = "> "
				row = cfgSelectedStyle.Render(fmt.Sprintf("%-16s  %-40s  %-10s  %s",
					typeLabel, truncate(item.Title, 40), item.Status, purpose))
				if item.AISuggestedPurpose != nil {
					row += cfgDimStyle.Render("  ai:" + *item.AISuggestedPurpose)
				}
			}
			sb.WriteString(prefix + row + "\n")
		}
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *ConfigSourcesView) footer() string {
	return "\n" + cfgDimStyle.Render(
		"  j/k navigate  ·  f filter  ·  / search  ·  Enter tag  ·  x ignore  ·  X ignore+children  ·  A classify  ·  D discover  ·  r reload  ·  Esc back",
	) + "\n"
}

// ---- ConfigTagView: inline tagging panel ----

var validPurposes = []string{"", "current_plan", "next_plan", "goals", "metrics_panel", "org_goals", "org_milestones"}

type configTagSavedMsg struct{ err error }

type configTagView struct {
	c          *client.Client
	item       client.SourceItemResponse
	teams      []client.TeamItem
	purposeIdx int // index into validPurposes (0 = none/ignore)
	teamIdx    int // index into teams (-1 = none)
	saving     bool
	errMsg     string
}

func newConfigTagView(c *client.Client, item client.SourceItemResponse, teams []client.TeamItem) *configTagView {
	purposeIdx := 0
	if len(item.Configs) > 0 && item.Configs[0].Purpose != "" {
		for i, p := range validPurposes {
			if p == item.Configs[0].Purpose {
				purposeIdx = i
				break
			}
		}
	}
	teamIdx := -1
	if len(item.Configs) > 0 && item.Configs[0].TeamID != nil {
		for i, t := range teams {
			if t.ID == *item.Configs[0].TeamID {
				teamIdx = i
				break
			}
		}
	}
	return &configTagView{c: c, item: item, teams: teams, purposeIdx: purposeIdx, teamIdx: teamIdx}
}

func (v *configTagView) Init() tea.Cmd { return nil }

func (v *configTagView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case configTagSavedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		case "tab":
			// Tab cycles through team assignment.
			v.teamIdx = (v.teamIdx + 1) % (len(v.teams) + 1)
			if v.teamIdx == len(v.teams) {
				v.teamIdx = -1
			}
			return v, nil
		case "left", "h":
			v.purposeIdx--
			if v.purposeIdx < 0 {
				v.purposeIdx = len(validPurposes) - 1
			}
			return v, nil
		case "right", "l":
			v.purposeIdx = (v.purposeIdx + 1) % len(validPurposes)
			return v, nil
		case "enter":
			if !v.saving {
				v.saving = true
				return v, v.save()
			}
		}
	}
	return v, nil
}

func (v *configTagView) save() tea.Cmd {
	c := v.c
	itemID := v.item.ID
	var purpose string
	var status string
	if v.item.SourceType == "github_label" {
		purpose = "task_label"
		if v.teamIdx >= 0 {
			status = "configured"
		} else {
			status = "ignored"
		}
	} else {
		purpose = validPurposes[v.purposeIdx]
		status = "configured"
		if purpose == "" {
			status = "ignored"
		}
	}
	var teamID *int64
	if v.teamIdx >= 0 && v.teamIdx < len(v.teams) {
		id := v.teams[v.teamIdx].ID
		teamID = &id
	}
	return func() tea.Msg {
		err := c.PutConfigSource(itemID, status, teamID, purpose, "")
		return configTagSavedMsg{err: err}
	}
}

func (v *configTagView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Tag Source: "+truncate(v.item.Title, 50)) + "\n\n")

	if v.item.AISuggestedPurpose != nil {
		suggestion := *v.item.AISuggestedPurpose
		if v.item.SourceType == "github_label" {
			sb.WriteString("  AI suggestion: " + cfgDimStyle.Render("team: "+suggestion) + "\n\n")
		} else {
			sb.WriteString("  AI suggestion: " + cfgDimStyle.Render(suggestion) + "\n\n")
		}
	}

	if v.item.SourceType == "github_label" {
		// Labels only need a team — purpose is always "task_label".
		sb.WriteString("  Purpose: " + cfgDimStyle.Render("task_label (fixed)") + "\n\n")
	} else {
		// Purpose selector (←/→).
		purposeLabel := validPurposes[v.purposeIdx]
		if purposeLabel == "" {
			purposeLabel = "(none — will ignore)"
		}
		sb.WriteString("  Purpose: " + cfgSelectedStyle.Render("← "+purposeLabel+" →") + "\n\n")
	}

	// Team selector (Tab).
	teamName := "(none)"
	if v.teamIdx >= 0 && v.teamIdx < len(v.teams) {
		teamName = v.teams[v.teamIdx].Name
	}
	sb.WriteString("  Team:    " + cfgSelectedStyle.Render(teamName) + "  " + cfgDimStyle.Render("(Tab to cycle)") + "\n")

	if v.errMsg != "" {
		sb.WriteString("\n  Error: " + v.errMsg + "\n")
	}
	if v.saving {
		sb.WriteString("\n  Saving…\n")
	}

	if v.item.SourceType == "github_label" {
		sb.WriteString("\n" + cfgDimStyle.Render("  Tab team  ·  Enter save  ·  Esc cancel") + "\n")
	} else {
		sb.WriteString("\n" + cfgDimStyle.Render("  ←/→ purpose  ·  Tab team  ·  Enter save  ·  Esc cancel") + "\n")
	}
	return sb.String()
}

// ---- ConfigDiscoverView: discovery prompt ----

// discoverSubmittedMsg is returned when the POST to start discovery succeeds.
type discoverSubmittedMsg struct {
	runID int64
	err   error
}

// discoverPollMsg triggers the next poll tick.
type discoverPollMsg struct{ runID int64 }

// discoverFinishedMsg is returned when the sync run reaches a terminal state.
type discoverFinishedMsg struct {
	errMsg string // empty on success
}

type configDiscoverView struct {
	c       *client.Client
	target  textinput.Model
	running bool  // POST in flight
	polling bool  // run is in progress on server
	runID   int64
	errMsg  string
}

func newConfigDiscoverView(c *client.Client) *configDiscoverView {
	ti := textinput.New()
	ti.Placeholder = "paste a URL or owner/repo"
	ti.Width = 70
	ti.Focus()
	return &configDiscoverView{c: c, target: ti}
}

func (v *configDiscoverView) Init() tea.Cmd {
	return textinput.Blink
}

func (v *configDiscoverView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case discoverSubmittedMsg:
		v.running = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		v.polling = true
		v.runID = m.runID
		return v, v.poll(m.runID)

	case discoverPollMsg:
		return v, v.poll(m.runID)

	case discoverFinishedMsg:
		v.polling = false
		// Pop this view then notify the sources list to reload.
		// tea.Sequence ensures PopViewMsg is processed first so the
		// subsequent discoverCompletedMsg reaches ConfigSourcesView.
		return v, tea.Sequence(
			func() tea.Msg { return msgs.PopViewMsg{} },
			func() tea.Msg { return discoverCompletedMsg{errMsg: m.errMsg} },
		)

	case tea.KeyMsg:
		if m.String() == "esc" && !v.polling {
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
		if !v.running && !v.polling {
			switch m.String() {
			case "enter":
				v.running = true
				v.errMsg = ""
				return v, v.submit()
			}
		}
	}
	if !v.running && !v.polling {
		var cmd tea.Cmd
		v.target, cmd = v.target.Update(msg)
		return v, cmd
	}
	return v, nil
}

func (v *configDiscoverView) submit() tea.Cmd {
	c := v.c
	target := strings.TrimSpace(v.target.Value())
	return func() tea.Msg {
		runID, err := c.PostDiscover("", target)
		return discoverSubmittedMsg{runID: runID, err: err}
	}
}

func (v *configDiscoverView) poll(runID int64) tea.Cmd {
	c := v.c
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return discoverFinishedMsg{errMsg: err.Error()}
		}
		switch run.Status {
		case "completed", "done":
			return discoverFinishedMsg{}
		case "failed", "error":
			errMsg := "discovery failed"
			if run.Error != nil {
				errMsg = *run.Error
			}
			return discoverFinishedMsg{errMsg: errMsg}
		default:
			return discoverPollMsg{runID: runID}
		}
	}
}

func (v *configDiscoverView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Discover Sources") + "\n\n")

	if v.polling {
		sb.WriteString(fmt.Sprintf("  Discovering… (run #%d)\n", v.runID))
		sb.WriteString(cfgDimStyle.Render("  This may take a moment. Please wait.") + "\n")
		return sb.String()
	}

	if v.running {
		sb.WriteString("  Starting discovery…\n")
		return sb.String()
	}

	sb.WriteString(cfgDimStyle.Render("  Accepts: GitHub project/repo URL · Notion URL · Grafana/PostHog/SigNoz URL · owner/repo") + "\n\n")
	sb.WriteString("  Target: " + v.target.View() + "\n")
	if v.errMsg != "" {
		sb.WriteString("\n  Error: " + v.errMsg + "\n")
	}
	sb.WriteString("\n" + cfgDimStyle.Render("  Enter to start  ·  Esc to cancel") + "\n")
	return sb.String()
}

// ---- helpers ----

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

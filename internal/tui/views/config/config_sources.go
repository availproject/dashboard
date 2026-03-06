package config

import (
	"fmt"
	"strings"

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

type sourcesDiscoverStartedMsg struct {
	runID int64
	err   error
}

// ---- status filter ----

var sourceStatusFilters = []string{"all", "active", "pending", "ignored"}

// ConfigSourcesView shows the source catalogue with tagging support.
type ConfigSourcesView struct {
	c           *client.Client
	items       []client.SourceItemResponse
	teams       []client.TeamItem
	loading     bool
	errMsg      string
	cursor      int
	filterIdx   int // index into sourceStatusFilters
	discoverMsg string
}

// NewConfigSourcesView creates a ConfigSourcesView.
func NewConfigSourcesView(c *client.Client) *ConfigSourcesView {
	return &ConfigSourcesView{c: c, loading: true}
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

func (v *ConfigSourcesView) filtered() []client.SourceItemResponse {
	filter := sourceStatusFilters[v.filterIdx]
	if filter == "all" {
		return v.items
	}
	var out []client.SourceItemResponse
	for _, it := range v.items {
		if it.Status == filter {
			out = append(out, it)
		}
	}
	return out
}

// Update implements tea.Model.
func (v *ConfigSourcesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
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

	case sourcesDiscoverStartedMsg:
		if m.err != nil {
			v.discoverMsg = "Discover error: " + m.err.Error()
		} else {
			v.discoverMsg = fmt.Sprintf("Discover started (run #%d). Reload to see new items.", m.runID)
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			filtered := v.filtered()
			if v.cursor < len(filtered)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "f":
			v.filterIdx = (v.filterIdx + 1) % len(sourceStatusFilters)
			v.cursor = 0
			return v, nil
		case "enter":
			return v, v.pushTagView()
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

func (v *ConfigSourcesView) pushTagView() tea.Cmd {
	filtered := v.filtered()
	if v.cursor < 0 || v.cursor >= len(filtered) {
		return nil
	}
	item := filtered[v.cursor]
	tv := newConfigTagView(v.c, item, v.teams)
	return func() tea.Msg { return msgs.PushViewMsg{View: tv} }
}

func (v *ConfigSourcesView) pushDiscoverPrompt() tea.Cmd {
	dv := newConfigDiscoverView(v.c)
	return func() tea.Msg { return msgs.PushViewMsg{View: dv} }
}


// View implements tea.Model.
func (v *ConfigSourcesView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config — Sources") + "\n\n")

	filter := sourceStatusFilters[v.filterIdx]
	sb.WriteString(cfgDimStyle.Render(fmt.Sprintf("  Filter: [%s]  (f to cycle)", filter)) + "\n\n")

	if v.discoverMsg != "" {
		sb.WriteString(cfgDimStyle.Render("  "+v.discoverMsg) + "\n\n")
	}
	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}
	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	filtered := v.filtered()
	if len(filtered) == 0 {
		sb.WriteString("  No items. Press D to run discovery.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	for i, item := range filtered {
		prefix := "  "
		typeLabel := "[" + item.SourceType + "]"
		purpose := item.Status
		if len(item.Configs) > 0 {
			purpose = item.Configs[0].Purpose
		}
		row := fmt.Sprintf("%-10s  %-40s  %-10s  %s", typeLabel, truncate(item.Title, 40), item.Status, purpose)
		if i == v.cursor {
			prefix = "> "
			row = cfgSelectedStyle.Render(row)
		}
		sb.WriteString(prefix + row + "\n")
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *ConfigSourcesView) footer() string {
	return "\n" + cfgDimStyle.Render("  j/k navigate  ·  f filter  ·  Enter tag  ·  D discover  ·  r reload  ·  Esc back") + "\n"
}

// ---- ConfigTagView: inline tagging panel ----

type configTagSavedMsg struct{ err error }

type configTagView struct {
	c       *client.Client
	item    client.SourceItemResponse
	teams   []client.TeamItem
	purpose textinput.Model
	teamIdx int // index into teams (-1 = none)
	saving  bool
	saved   bool
	errMsg  string
}

func newConfigTagView(c *client.Client, item client.SourceItemResponse, teams []client.TeamItem) *configTagView {
	ti := textinput.New()
	ti.Width = 40
	ti.Focus()
	if len(item.Configs) > 0 {
		ti.SetValue(item.Configs[0].Purpose)
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
	return &configTagView{c: c, item: item, teams: teams, purpose: ti, teamIdx: teamIdx}
}

func (v *configTagView) Init() tea.Cmd {
	return textinput.Blink
}

func (v *configTagView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case configTagSavedMsg:
		v.saving = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.saved = true
			// pop this view after save
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		case "tab":
			// cycle team assignment
			v.teamIdx = (v.teamIdx + 1) % (len(v.teams) + 1)
			if v.teamIdx == len(v.teams) {
				v.teamIdx = -1
			}
			return v, nil
		case "enter":
			if !v.saving {
				v.saving = true
				return v, v.save()
			}
		}
	}
	var cmd tea.Cmd
	v.purpose, cmd = v.purpose.Update(msg)
	return v, cmd
}

func (v *configTagView) save() tea.Cmd {
	c := v.c
	itemID := v.item.ID
	purpose := strings.TrimSpace(v.purpose.Value())
	status := "active"
	if purpose == "" {
		status = "ignored"
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
		sb.WriteString("  AI suggestion: " + cfgDimStyle.Render(*v.item.AISuggestedPurpose) + "\n\n")
	}

	// Team selector.
	teamName := "(none)"
	if v.teamIdx >= 0 && v.teamIdx < len(v.teams) {
		teamName = v.teams[v.teamIdx].Name
	}
	sb.WriteString("  Team: " + cfgSelectedStyle.Render(teamName) + "  " + cfgDimStyle.Render("(Tab to cycle)") + "\n\n")
	sb.WriteString("  Purpose: " + v.purpose.View() + "\n")

	if v.errMsg != "" {
		sb.WriteString("\n  Error: " + v.errMsg + "\n")
	}
	if v.saving {
		sb.WriteString("\n  Saving…\n")
	}

	sb.WriteString("\n" + cfgDimStyle.Render("  Enter to save  ·  Tab to cycle team  ·  Esc to cancel") + "\n")
	return sb.String()
}

// ---- ConfigDiscoverView: discovery prompt ----

type discoverDoneMsg struct {
	runID int64
	err   error
}

type configDiscoverView struct {
	c      *client.Client
	target textinput.Model
	scope  string // "team" | "org"
	running bool
	errMsg  string
}

func newConfigDiscoverView(c *client.Client) *configDiscoverView {
	ti := textinput.New()
	ti.Placeholder = "https://notion.so/... or github.com/org/repo"
	ti.Width = 60
	ti.Focus()
	return &configDiscoverView{c: c, target: ti, scope: "org"}
}

func (v *configDiscoverView) Init() tea.Cmd {
	return textinput.Blink
}

func (v *configDiscoverView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case discoverDoneMsg:
		v.running = false
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
			if v.scope == "org" {
				v.scope = "team"
			} else {
				v.scope = "org"
			}
			return v, nil
		case "enter":
			if !v.running {
				v.running = true
				return v, v.runDiscover()
			}
		}
	}
	var cmd tea.Cmd
	v.target, cmd = v.target.Update(msg)
	return v, cmd
}

func (v *configDiscoverView) runDiscover() tea.Cmd {
	c := v.c
	target := strings.TrimSpace(v.target.Value())
	scope := v.scope
	return func() tea.Msg {
		runID, err := c.PostDiscover(scope, target)
		return discoverDoneMsg{runID: runID, err: err}
	}
}

func (v *configDiscoverView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Discover Sources") + "\n\n")
	sb.WriteString("  Scope: " + cfgSelectedStyle.Render(v.scope) + "  " + cfgDimStyle.Render("(Tab to toggle)") + "\n\n")
	sb.WriteString("  Target URL: " + v.target.View() + "\n")
	if v.errMsg != "" {
		sb.WriteString("\n  Error: " + v.errMsg + "\n")
	}
	if v.running {
		sb.WriteString("\n  Running discovery…\n")
	}
	sb.WriteString("\n" + cfgDimStyle.Render("  Enter to start  ·  Tab toggle scope  ·  Esc to cancel") + "\n")
	return sb.String()
}

// ---- helpers ----

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

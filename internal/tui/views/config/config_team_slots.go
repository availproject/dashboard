package config

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- message types ----

type teamSlotsLoadedMsg struct {
	data *client.TeamConfigSlotsResponse
	err  error
}

type teamHomepageSetMsg struct {
	runID int64
	err   error
}

type teamExtractPollMsg struct{ runID int64 }

type teamExtractDoneMsg struct{ err error }

// slotDisplayOrder defines the order slots are shown in the view.
var slotDisplayOrder = []string{
	"project_homepage",
	"goals_doc",
	"sprint_doc",
	"github_repo",
	"github_project",
	"metrics_panel",
	"marketing_calendar",
}

var slotLabels = map[string]string{
	"project_homepage":   "Project Homepage",
	"goals_doc":          "Goals / GTM Doc",
	"sprint_doc":         "Sprint Plans",
	"github_repo":        "GitHub Repos",
	"github_project":     "Project Board",
	"metrics_panel":      "Metrics",
	"marketing_calendar": "Marketing Calendar",
}

// slotIsAuto indicates which slots are auto-configured from the homepage.
var slotIsAuto = map[string]bool{
	"goals_doc":      true,
	"sprint_doc":     true,
	"github_repo":    true,
	"github_project": true,
	"metrics_panel":  true,
}

// ConfigTeamSlotsView shows the team slot configuration overview.
type ConfigTeamSlotsView struct {
	c          *client.Client
	teamID     int64
	teamName   string
	data       *client.TeamConfigSlotsResponse
	loading    bool
	errMsg     string
	cursor     int
	// extraction polling
	extractRunID int64
	extracting   bool
	extractMsg   string
}

// NewConfigTeamSlotsView creates a ConfigTeamSlotsView for the given team.
func NewConfigTeamSlotsView(c *client.Client, teamID int64, teamName string) *ConfigTeamSlotsView {
	return &ConfigTeamSlotsView{
		c:        c,
		teamID:   teamID,
		teamName: teamName,
		loading:  true,
	}
}

// Init implements tea.Model.
func (v *ConfigTeamSlotsView) Init() tea.Cmd {
	return v.loadData()
}

func (v *ConfigTeamSlotsView) loadData() tea.Cmd {
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		data, err := c.GetTeamConfig(teamID)
		return teamSlotsLoadedMsg{data: data, err: err}
	}
}

// Update implements tea.Model.
func (v *ConfigTeamSlotsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case teamSlotsLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.data = m.data
			if v.data != nil {
				v.teamName = v.data.TeamName
			}
		}
		return v, nil

	case teamExtractPollMsg:
		return v, v.pollExtract(m.runID)

	case teamExtractDoneMsg:
		v.extracting = false
		if m.err != nil {
			v.extractMsg = "Extraction failed: " + m.err.Error()
		} else {
			v.extractMsg = "Extraction complete."
		}
		v.loading = true
		return v, v.loadData()

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.cursor < len(slotDisplayOrder)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "enter":
			return v, v.pushSlotView()
		case "e":
			if !v.extracting && v.data != nil && len(v.data.Slots["project_homepage"]) > 0 {
				return v, v.triggerExtract()
			}
			return v, nil
		case "r":
			v.loading = true
			return v, v.loadData()
		}
	}
	return v, nil
}

func (v *ConfigTeamSlotsView) triggerExtract() tea.Cmd {
	c := v.c
	teamID := v.teamID
	v.extracting = true
	v.extractMsg = "Analyzing homepage…"
	return func() tea.Msg {
		runID, err := c.PostTeamReextract(teamID)
		if err != nil {
			return teamExtractDoneMsg{err: err}
		}
		return teamExtractPollMsg{runID: runID}
	}
}

func (v *ConfigTeamSlotsView) pollExtract(runID int64) tea.Cmd {
	c := v.c
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return teamExtractDoneMsg{err: err}
		}
		switch run.Status {
		case "completed", "done":
			return teamExtractDoneMsg{}
		case "failed", "error":
			errMsg := "extraction failed"
			if run.Error != nil {
				errMsg = *run.Error
			}
			return teamExtractDoneMsg{err: fmt.Errorf("%s", errMsg)}
		default:
			return teamExtractPollMsg{runID: runID}
		}
	}
}

func (v *ConfigTeamSlotsView) pushSlotView() tea.Cmd {
	if v.data == nil {
		return nil
	}
	purpose := slotDisplayOrder[v.cursor]
	items := v.data.Slots[purpose]

	var subView tea.Model
	switch purpose {
	case "project_homepage":
		var current *client.TeamConfigSlotItem
		if len(items) > 0 {
			current = &items[0]
		}
		subView = NewConfigHomepageSlotView(v.c, v.teamID, v.teamName, current)
	case "github_project":
		subView = NewConfigBoardSlotView(v.c, v.teamID, v.teamName, items)
	case "goals_doc":
		subView = NewConfigSingleSlotView(v.c, v.teamID, v.teamName, purpose, items)
	case "marketing_calendar":
		var currentLabel *string
		if v.data != nil {
			currentLabel = v.data.MarketingLabel
		}
		subView = NewConfigMarketingSlotView(v.c, v.teamID, v.teamName, items, currentLabel)
	default:
		subView = NewConfigMultiSlotView(v.c, v.teamID, v.teamName, purpose, items)
	}
	return func() tea.Msg { return msgs.PushViewMsg{View: subView} }
}

// View implements tea.Model.
func (v *ConfigTeamSlotsView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — "+v.teamName) + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}
	if v.extractMsg != "" {
		sb.WriteString(cfgDimStyle.Render("  "+v.extractMsg) + "\n\n")
	}

	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.slotFooter())
		return sb.String()
	}

	homepageSet := v.data != nil && len(v.data.Slots["project_homepage"]) > 0
	autoSectionShown := false

	for i, purpose := range slotDisplayOrder {
		prefix := "  "
		isSelected := i == v.cursor

		// Show divider before auto section.
		if slotIsAuto[purpose] && !autoSectionShown {
			autoSectionShown = true
			if homepageSet {
				sb.WriteString("\n  " + cfgDimStyle.Render("─── Auto-configured from homepage ─────────") + "\n")
			} else {
				sb.WriteString("\n  " + cfgDimStyle.Render("─── Will be auto-configured from homepage ──") + "\n")
			}
		}
		// Show divider before marketing section.
		if purpose == "marketing_calendar" {
			sb.WriteString("\n  " + cfgDimStyle.Render("─── Marketing ───────────────────────────────") + "\n")
		}

		label := slotLabels[purpose]
		summary := v.slotSummary(purpose)
		check := "○"
		if v.data != nil && len(v.data.Slots[purpose]) > 0 {
			check = "✓"
		}

		if isSelected {
			prefix = "> "
			row := cfgSelectedStyle.Render(fmt.Sprintf("%-22s  %-40s  %s", label, summary, check))
			sb.WriteString(prefix + row + "\n")
		} else {
			row := fmt.Sprintf("  %-22s  %-40s  %s", label, summary, check)
			sb.WriteString(row + "\n")
		}
	}

	sb.WriteString(v.slotFooter())
	return sb.String()
}

func (v *ConfigTeamSlotsView) slotSummary(purpose string) string {
	if v.data == nil {
		return ""
	}
	items := v.data.Slots[purpose]
	switch purpose {
	case "project_homepage":
		if len(items) == 0 {
			return "(not set — start here)"
		}
		return truncate(items[0].Title, 40)
	case "goals_doc":
		if len(items) == 0 {
			return "(none)"
		}
		return truncate(items[0].Title, 40)
	case "sprint_doc":
		if len(items) == 0 {
			return "(none)"
		}
		var current string
		for _, it := range items {
			if it.SprintStatus != nil && *it.SprintStatus == "current" {
				current = truncate(it.Title, 30)
				break
			}
		}
		if current == "" {
			current = truncate(items[0].Title, 30)
		}
		extra := len(items) - 1
		if extra > 0 {
			return fmt.Sprintf("%s (+%d more)", current, extra)
		}
		return current
	case "github_repo":
		if len(items) == 0 {
			return "(none)"
		}
		first := items[0].Title
		if items[0].URL != nil {
			first = *items[0].URL
		}
		extra := len(items) - 1
		if extra > 0 {
			return fmt.Sprintf("%s (+%d more)", truncate(first, 30), extra)
		}
		return truncate(first, 40)
	case "github_project":
		if len(items) == 0 {
			return "(none)"
		}
		bc := items[0].BoardConfig
		if bc != nil && bc.TeamAreaValue != "" {
			return truncate(fmt.Sprintf("%s · %s", items[0].Title, bc.TeamAreaValue), 40)
		}
		return truncate(items[0].Title, 40)
	case "metrics_panel":
		if len(items) == 0 {
			if v.data.ExtractionStatus == "done" {
				return "(none found — add manually)"
			}
			return "(none)"
		}
		extra := len(items) - 1
		label := truncate(items[0].Title, 30)
		if extra > 0 {
			return fmt.Sprintf("%s (+%d more)", label, extra)
		}
		return label
	case "marketing_calendar":
		if len(items) == 0 {
			return "(none)"
		}
		return truncate(items[0].Title, 40)
	}
	return ""
}

func (v *ConfigTeamSlotsView) slotFooter() string {
	return "\n" + cfgDimStyle.Render(
		"  j/k navigate  ·  Enter edit  ·  e re-extract  ·  r reload  ·  Esc back",
	) + "\n"
}

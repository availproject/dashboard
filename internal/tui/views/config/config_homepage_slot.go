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

type homepagePickedMsg struct{ catalogueID int64 }
type homepageSetMsg struct {
	runID int64
	err   error
}
type homepagePollMsg struct{ runID int64 }
type homepageDoneMsg struct{ err error }

// homepageState describes what the homepage slot view is currently showing.
type homepageState int

const (
	homepageStateNormal homepageState = iota
	homepageStateConfirmReplace
	homepageStateSetting
	homepageStateExtracting
	homepageStateError
)

// ConfigHomepageSlotView manages the "Project Homepage" slot.
type ConfigHomepageSlotView struct {
	c        *client.Client
	teamID   int64
	teamName string
	current  *client.TeamConfigSlotItem
	state    homepageState
	errMsg   string
	// pending: when replacing, the picked item waiting for confirmation.
	pendingCatalogueID int64
	pendingTitle       string
}

// NewConfigHomepageSlotView creates a ConfigHomepageSlotView.
func NewConfigHomepageSlotView(c *client.Client, teamID int64, teamName string, current *client.TeamConfigSlotItem) *ConfigHomepageSlotView {
	return &ConfigHomepageSlotView{
		c:        c,
		teamID:   teamID,
		teamName: teamName,
		current:  current,
	}
}

// Init implements tea.Model.
func (v *ConfigHomepageSlotView) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (v *ConfigHomepageSlotView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case msgs.SourcePickedMsg:
		if v.current == nil {
			// No existing homepage — set immediately.
			v.state = homepageStateSetting
			return v, v.setHomepage(m.Item.ID)
		}
		// Existing homepage — confirm replacement.
		v.pendingCatalogueID = m.Item.ID
		v.pendingTitle = m.Item.Title
		v.state = homepageStateConfirmReplace
		return v, nil

	case homepageSetMsg:
		if m.err != nil {
			v.state = homepageStateError
			v.errMsg = m.err.Error()
			return v, nil
		}
		v.state = homepageStateExtracting
		return v, v.pollExtract(m.runID)

	case homepagePollMsg:
		return v, v.pollExtract(m.runID)

	case homepageDoneMsg:
		v.state = homepageStateNormal
		if m.err != nil {
			v.errMsg = m.err.Error()
			v.state = homepageStateError
		}
		// Pop view so parent reloads.
		return v, func() tea.Msg { return msgs.PopViewMsg{} }

	case tea.KeyMsg:
		switch v.state {
		case homepageStateConfirmReplace:
			switch m.String() {
			case "y", "enter":
				v.state = homepageStateSetting
				return v, v.setHomepage(v.pendingCatalogueID)
			case "n", "esc":
				v.state = homepageStateNormal
				v.pendingCatalogueID = 0
				v.pendingTitle = ""
			}
		case homepageStateNormal, homepageStateError:
			switch m.String() {
			case "enter":
				return v, v.pushPicker()
			case "esc":
				return v, func() tea.Msg { return msgs.PopViewMsg{} }
			}
		}
	}
	return v, nil
}

func (v *ConfigHomepageSlotView) pushPicker() tea.Cmd {
	picker := NewConfigSourcePickerView(v.c, []string{"notion_page", "github_file"})
	return func() tea.Msg { return msgs.PushViewMsg{View: picker} }
}

func (v *ConfigHomepageSlotView) setHomepage(catalogueID int64) tea.Cmd {
	c := v.c
	teamID := v.teamID
	return func() tea.Msg {
		runID, err := c.PostTeamHomepage(teamID, catalogueID)
		return homepageSetMsg{runID: runID, err: err}
	}
}

func (v *ConfigHomepageSlotView) pollExtract(runID int64) tea.Cmd {
	c := v.c
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return homepageDoneMsg{err: err}
		}
		switch run.Status {
		case "completed", "done":
			return homepageDoneMsg{}
		case "failed", "error":
			errMsg := "extraction failed"
			if run.Error != nil {
				errMsg = *run.Error
			}
			return homepageDoneMsg{err: fmt.Errorf("%s", errMsg)}
		default:
			return homepagePollMsg{runID: runID}
		}
	}
}

// View implements tea.Model.
func (v *ConfigHomepageSlotView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Configure — "+v.teamName+" — Project Homepage") + "\n\n")

	switch v.state {
	case homepageStateSetting:
		sb.WriteString("  Setting homepage…\n")
		return sb.String()

	case homepageStateExtracting:
		sb.WriteString("  Homepage set. Analyzing for auto-config…\n")
		sb.WriteString(cfgDimStyle.Render("  This may take a moment. Please wait.") + "\n")
		return sb.String()

	case homepageStateConfirmReplace:
		sb.WriteString("  Replace current homepage?\n\n")
		if v.current != nil {
			sb.WriteString(cfgDimStyle.Render("  Current: "+v.current.Title) + "\n")
		}
		sb.WriteString("  New:     " + v.pendingTitle + "\n\n")
		sb.WriteString("  This will trigger a fresh auto-extraction.\n\n")
		sb.WriteString(cfgDimStyle.Render("  y/Enter confirm  ·  n/Esc cancel") + "\n")
		return sb.String()

	case homepageStateError:
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to pick again  ·  Esc back") + "\n")
		return sb.String()
	}

	// Normal state.
	if v.current == nil {
		sb.WriteString("  No homepage set.\n\n")
		sb.WriteString("  The Project Homepage is the central doc where your team links to sprint plans,\n")
		sb.WriteString("  goals, and repos. Setting it lets the dashboard auto-configure your team slots.\n\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to pick a source  ·  Esc back") + "\n")
	} else {
		sb.WriteString("  Current Homepage:\n\n")
		sb.WriteString("    " + cfgSelectedStyle.Render(v.current.Title) + "\n")
		sb.WriteString("    " + cfgDimStyle.Render(v.current.SourceType))
		if v.current.URL != nil {
			sb.WriteString("  ·  " + cfgDimStyle.Render(*v.current.URL))
		}
		sb.WriteString("\n\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to replace  ·  Esc back") + "\n")
	}
	return sb.String()
}

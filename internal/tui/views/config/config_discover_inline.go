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

// ---- message types ----

type inlineDiscoverSubmittedMsg struct {
	runID int64
	err   error
}

type inlineDiscoverPollMsg struct{ runID int64 }

type inlineDiscoverFinishedMsg struct {
	errMsg string
}

type inlineDiscoverItemsLoadedMsg struct {
	items []client.SourceItemResponse
	err   error
}

// ConfigDiscoverInlineView allows the user to enter a URL, discover it,
// and then select items to add to a slot.
type ConfigDiscoverInlineView struct {
	c            *client.Client
	target       textinput.Model
	running      bool
	polling      bool
	runID        int64
	errMsg       string
	// after discovery
	discoveredItems []client.SourceItemResponse
	selected        map[int]bool
	cursor          int
}

// NewConfigDiscoverInlineView creates a ConfigDiscoverInlineView.
func NewConfigDiscoverInlineView(c *client.Client) *ConfigDiscoverInlineView {
	ti := textinput.New()
	ti.Placeholder = "paste a URL or owner/repo"
	ti.Width = 60
	ti.Focus()
	return &ConfigDiscoverInlineView{
		c:        c,
		target:   ti,
		selected: make(map[int]bool),
	}
}

// Init implements tea.Model.
func (v *ConfigDiscoverInlineView) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (v *ConfigDiscoverInlineView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case inlineDiscoverSubmittedMsg:
		v.running = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		v.polling = true
		v.runID = m.runID
		return v, v.poll(m.runID)

	case inlineDiscoverPollMsg:
		return v, v.poll(m.runID)

	case inlineDiscoverFinishedMsg:
		v.polling = false
		if m.errMsg != "" {
			v.errMsg = m.errMsg
			return v, nil
		}
		// Load discovered items.
		return v, v.loadItems()

	case inlineDiscoverItemsLoadedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		v.discoveredItems = m.items
		v.selected = make(map[int]bool)
		v.cursor = 0
		return v, nil

	case tea.KeyMsg:
		// If items are loaded, handle selection.
		if len(v.discoveredItems) > 0 {
			switch m.String() {
			case "j", "down":
				if v.cursor < len(v.discoveredItems)-1 {
					v.cursor++
				}
				return v, nil
			case "k", "up":
				if v.cursor > 0 {
					v.cursor--
				}
				return v, nil
			case " ":
				v.selected[v.cursor] = !v.selected[v.cursor]
				return v, nil
			case "enter":
				return v, v.confirmSelection()
			case "esc":
				return v, func() tea.Msg { return msgs.PopViewMsg{} }
			}
			return v, nil
		}

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

	if !v.running && !v.polling && len(v.discoveredItems) == 0 {
		var cmd tea.Cmd
		v.target, cmd = v.target.Update(msg)
		return v, cmd
	}
	return v, nil
}

func (v *ConfigDiscoverInlineView) submit() tea.Cmd {
	c := v.c
	target := strings.TrimSpace(v.target.Value())
	return func() tea.Msg {
		runID, err := c.PostDiscover("", target)
		return inlineDiscoverSubmittedMsg{runID: runID, err: err}
	}
}

func (v *ConfigDiscoverInlineView) poll(runID int64) tea.Cmd {
	c := v.c
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return inlineDiscoverFinishedMsg{errMsg: err.Error()}
		}
		switch run.Status {
		case "completed", "done":
			return inlineDiscoverFinishedMsg{}
		case "failed", "error":
			errMsg := "discovery failed"
			if run.Error != nil {
				errMsg = *run.Error
			}
			return inlineDiscoverFinishedMsg{errMsg: errMsg}
		default:
			return inlineDiscoverPollMsg{runID: runID}
		}
	}
}

func (v *ConfigDiscoverInlineView) loadItems() tea.Cmd {
	c := v.c
	return func() tea.Msg {
		items, err := c.GetConfigSources()
		return inlineDiscoverItemsLoadedMsg{items: items, err: err}
	}
}

func (v *ConfigDiscoverInlineView) confirmSelection() tea.Cmd {
	var selected []client.SourceItemResponse
	for i, item := range v.discoveredItems {
		if v.selected[i] {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 && v.cursor < len(v.discoveredItems) {
		selected = []client.SourceItemResponse{v.discoveredItems[v.cursor]}
	}
	return tea.Sequence(
		func() tea.Msg { return msgs.PopViewMsg{} },
		func() tea.Msg { return msgs.DiscoveredItemsSelectedMsg{Items: selected} },
	)
}

// View implements tea.Model.
func (v *ConfigDiscoverInlineView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Discover a New Source") + "\n\n")

	if v.polling {
		sb.WriteString(fmt.Sprintf("  Discovering… (run #%d)\n", v.runID))
		sb.WriteString(cfgDimStyle.Render("  This may take a moment.") + "\n")
		return sb.String()
	}

	if v.running {
		sb.WriteString("  Starting discovery…\n")
		return sb.String()
	}

	// Show discovered items for selection.
	if len(v.discoveredItems) > 0 {
		sb.WriteString("  Select items to add:\n\n")
		for i, item := range v.discoveredItems {
			prefix := "    "
			check := "[ ]"
			if v.selected[i] {
				check = "[x]"
			}
			line := fmt.Sprintf("%s  %-16s  %s", check, "["+item.SourceType+"]", truncate(item.Title, 50))
			if i == v.cursor {
				sb.WriteString("> " + cfgSelectedStyle.Render(prefix[2:]+line) + "\n")
			} else {
				sb.WriteString(prefix + line + "\n")
			}
		}
		sb.WriteString("\n" + cfgDimStyle.Render("  j/k navigate  ·  Space toggle  ·  Enter confirm  ·  Esc cancel") + "\n")
		return sb.String()
	}

	// Input mode.
	sb.WriteString(cfgDimStyle.Render("  Accepts: GitHub repo URL · Notion URL · owner/repo") + "\n\n")
	sb.WriteString("  URL: " + v.target.View() + "\n")
	if v.errMsg != "" {
		sb.WriteString("\n  Error: " + v.errMsg + "\n")
	}
	sb.WriteString("\n" + cfgDimStyle.Render("  Enter to discover  ·  Esc to cancel") + "\n")
	return sb.String()
}

package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
)

// ---- message types ----

type pickerLoadedMsg struct {
	items []client.SourceItemResponse
	err   error
}

// ConfigSourcePickerView is a filterable list of catalogue items.
// When the user selects an item, it sends SourcePickedMsg and pops itself.
type ConfigSourcePickerView struct {
	c               *client.Client
	compatibleTypes []string
	items           []client.SourceItemResponse
	loading         bool
	errMsg          string
	cursor          int
	scrollOffset    int
	height          int
	search          textinput.Model
	searchMode      bool
}

// NewConfigSourcePickerView creates a ConfigSourcePickerView.
func NewConfigSourcePickerView(c *client.Client, compatibleTypes []string) *ConfigSourcePickerView {
	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Width = 50
	return &ConfigSourcePickerView{
		c:               c,
		compatibleTypes: compatibleTypes,
		loading:         true,
		search:          ti,
	}
}

// Init implements tea.Model.
func (v *ConfigSourcePickerView) Init() tea.Cmd {
	return v.loadItems()
}

func (v *ConfigSourcePickerView) loadItems() tea.Cmd {
	c := v.c
	types := v.compatibleTypes
	return func() tea.Msg {
		items, err := c.GetConfigSources(types...)
		return pickerLoadedMsg{items: items, err: err}
	}
}

func (v *ConfigSourcePickerView) filtered() []client.SourceItemResponse {
	q := strings.ToLower(strings.TrimSpace(v.search.Value()))
	if q == "" {
		return v.items
	}
	var out []client.SourceItemResponse
	for _, it := range v.items {
		if strings.Contains(strings.ToLower(it.Title), q) {
			out = append(out, it)
		}
	}
	return out
}

// Update implements tea.Model.
func (v *ConfigSourcePickerView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		v.height = m.Height
		return v, nil

	case pickerLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.items = m.items
		}
		return v, nil

	case tea.KeyMsg:
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

		switch m.String() {
		case "j", "down":
			filtered := v.filtered()
			if v.cursor < len(filtered) { // len(filtered) for "Discover new source" at end
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
		case "/":
			v.searchMode = true
			v.search.Focus()
			return v, textinput.Blink
		case "enter":
			return v, v.selectItem()
		case "esc":
			return v, func() tea.Msg { return msgs.PopViewMsg{} }
		}
	}
	return v, nil
}

func (v *ConfigSourcePickerView) selectItem() tea.Cmd {
	filtered := v.filtered()
	if v.cursor == len(filtered) {
		// "Discover a new source" option.
		dv := NewConfigDiscoverInlineView(v.c)
		return func() tea.Msg { return msgs.PushViewMsg{View: dv} }
	}
	if v.cursor >= len(filtered) {
		return nil
	}
	item := filtered[v.cursor]
	return tea.Sequence(
		func() tea.Msg { return msgs.PopViewMsg{} },
		func() tea.Msg { return msgs.SourcePickedMsg{Item: item} },
	)
}

func (v *ConfigSourcePickerView) availableLines() int {
	if v.height <= 0 {
		return 20
	}
	return max(5, v.height-8)
}

func (v *ConfigSourcePickerView) clampScroll() {
	avail := v.availableLines()
	if v.cursor < v.scrollOffset {
		v.scrollOffset = v.cursor
	}
	if v.cursor >= v.scrollOffset+avail {
		v.scrollOffset = v.cursor - avail + 1
	}
}

// View implements tea.Model.
func (v *ConfigSourcePickerView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Pick a Source") + "\n\n")

	if v.searchMode {
		sb.WriteString("  Search: " + v.search.View() + "\n\n")
	} else if v.search.Value() != "" {
		sb.WriteString(cfgDimStyle.Render("  Search: "+v.search.Value()+"  (/ to edit, Esc to clear)") + "\n\n")
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
	avail := v.availableLines()
	total := len(filtered) + 1 // +1 for "Discover" option

	start := v.scrollOffset
	end := min(start+avail, total)

	for i := start; i < end; i++ {
		prefix := "  "
		if i == v.cursor {
			prefix = "> "
		}
		if i == len(filtered) {
			// "Discover" option.
			line := fmt.Sprintf("+ Discover a new source…")
			if i == v.cursor {
				sb.WriteString(prefix + cfgSelectedStyle.Render(line) + "\n")
			} else {
				sb.WriteString(prefix + cfgDimStyle.Render(line) + "\n")
			}
			continue
		}

		item := filtered[i]
		typeLabel := "[" + item.SourceType + "]"
		line := fmt.Sprintf("%-16s  %s", typeLabel, truncate(item.Title, 55))
		if i == v.cursor {
			sb.WriteString(prefix + cfgSelectedStyle.Render(line) + "\n")
		} else {
			sb.WriteString(prefix + line + "\n")
		}
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *ConfigSourcePickerView) footer() string {
	return "\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter select  ·  / search  ·  Esc cancel") + "\n"
}

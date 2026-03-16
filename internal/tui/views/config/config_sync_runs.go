package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/your-org/dashboard/internal/tui/client"
)

var (
	syncGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	syncYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	syncRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// ── timing tree ──────────────────────────────────────────────────────────────

// timingNode is one node in the colon-split key hierarchy.
// e.g. "github:github_repo:org/repo:commits_ms" → path [github][github_repo][org/repo][commits]
type timingNode struct {
	label    string                   // single path segment (last colon-part, _ms stripped)
	ms       int64                    // own timing value; 0 for synthetic container nodes
	children []*timingNode
	childMap map[string]*timingNode   // used during construction only
	expanded bool
}

// effectiveMs returns ms for leaf nodes, or the max-leaf effectiveMs for containers.
func effectiveMs(n *timingNode) int64 {
	if len(n.children) == 0 {
		return n.ms
	}
	if n.ms > 0 {
		return n.ms
	}
	var best int64
	for _, c := range n.children {
		if e := effectiveMs(c); e > best {
			best = e
		}
	}
	return best
}

// sumLeafMs returns the sum of all leaf ms values under n.
func sumLeafMs(n *timingNode) int64 {
	if len(n.children) == 0 {
		return n.ms
	}
	var total int64
	for _, c := range n.children {
		total += sumLeafMs(c)
	}
	return total
}

// buildTimingTree constructs a tree from the flat timing map.
// Keys are split on ":" and _ms is stripped from the last segment.
// Synthetic containers (no own ms, created from colon-prefixes) are merged
// with matching flat keys so that e.g. "phase2" becomes the expandable node
// rather than a separate sibling of the "pipeline" container.
func buildTimingTree(timings map[string]int64) []*timingNode {
	root := &timingNode{childMap: map[string]*timingNode{}}

	for key, ms := range timings {
		k := strings.TrimSuffix(key, "_ms")
		parts := strings.Split(k, ":")

		cur := root
		for i, part := range parts {
			child, ok := cur.childMap[part]
			if !ok {
				child = &timingNode{
					label:    part,
					childMap: map[string]*timingNode{},
				}
				cur.childMap[part] = child
				cur.children = append(cur.children, child)
			}
			if i == len(parts)-1 {
				child.ms = ms
			}
			cur = child
		}
	}

	// Merge synthetic containers at the root level with matching flat keys.
	// Pass 1: match by max-leaf ms (effectiveMs) within 5% — catches exact
	//         aggregate pairs like pipeline↔phase2 and content↔content_fetch.
	// Pass 2: match by sum-leaf ms within 5% — catches parallel-fetch pairs
	//         like github↔github_fetch.
	root.children = mergeContainers(root.children, effectiveMs, 1.05)
	root.children = mergeContainers(root.children, sumLeafMs, 1.05)

	// Sort each level by effectiveMs descending.
	var sortLevel func([]*timingNode)
	sortLevel = func(nodes []*timingNode) {
		sort.Slice(nodes, func(i, j int) bool {
			return effectiveMs(nodes[i]) > effectiveMs(nodes[j])
		})
		for _, n := range nodes {
			sortLevel(n.children)
		}
	}
	sortLevel(root.children)

	return root.children
}

// mergeContainers merges synthetic containers (no own ms, has children) with
// flat sibling keys whose ms is within the given ratio of metric(container).
// The flat key's label and ms are adopted by the container; the flat key is removed.
func mergeContainers(nodes []*timingNode, metric func(*timingNode) int64, maxRatio float64) []*timingNode {
	var containers, flats []*timingNode
	for _, n := range nodes {
		if n.ms == 0 && len(n.children) > 0 {
			containers = append(containers, n)
		} else {
			flats = append(flats, n)
		}
	}

	absorbed := make(map[*timingNode]bool)
	for _, c := range containers {
		ref := metric(c)
		if ref == 0 {
			continue
		}
		var best *timingNode
		var bestRatio float64
		for _, f := range flats {
			if f.ms == 0 || absorbed[f] {
				continue
			}
			lo, hi := ref, f.ms
			if lo > hi {
				lo, hi = hi, lo
			}
			r := float64(hi) / float64(lo)
			if r <= maxRatio && (best == nil || r < bestRatio) {
				best, bestRatio = f, r
			}
		}
		if best != nil {
			c.ms = best.ms
			c.label = best.label
			absorbed[best] = true
		}
	}

	result := make([]*timingNode, 0, len(nodes))
	for _, n := range nodes {
		if !absorbed[n] {
			result = append(result, n)
		}
	}
	return result
}

// visibleItem is one row in the flattened tree view.
type visibleItem struct {
	node  *timingNode
	depth int
}

// flattenVisible returns the currently visible rows (respecting expansion state).
func flattenVisible(nodes []*timingNode, depth int) []visibleItem {
	var out []visibleItem
	for _, n := range nodes {
		out = append(out, visibleItem{node: n, depth: depth})
		if n.expanded && len(n.children) > 0 {
			out = append(out, flattenVisible(n.children, depth+1)...)
		}
	}
	return out
}

// ── messages ─────────────────────────────────────────────────────────────────

type syncRunsLoadedMsg struct {
	runs []client.SyncRunListItem
	err  error
}

// ── view ─────────────────────────────────────────────────────────────────────

type syncRunsMode int

const (
	syncRunsModeList syncRunsMode = iota
	syncRunsModeDetail
)

// ConfigSyncRunsView lets you browse recent sync runs and inspect their timings.
type ConfigSyncRunsView struct {
	c            *client.Client
	runs         []client.SyncRunListItem
	loading      bool
	errMsg       string
	cursor       int
	scrollOffset int
	height       int
	mode         syncRunsMode

	// detail-mode state
	detailNodes  []*timingNode // root nodes for current run's tree
	detailCursor int           // index in flattenVisible result
	detailScroll int           // top-visible row in the timing list
	detailMaxMS  int64         // global max ms for bar scaling
}

// NewConfigSyncRunsView creates a ConfigSyncRunsView.
func NewConfigSyncRunsView(c *client.Client) *ConfigSyncRunsView {
	return &ConfigSyncRunsView{c: c, loading: true}
}

// Init implements tea.Model.
func (v *ConfigSyncRunsView) Init() tea.Cmd {
	return v.load()
}

func (v *ConfigSyncRunsView) load() tea.Cmd {
	c := v.c
	return func() tea.Msg {
		runs, err := c.ListSyncRuns()
		return syncRunsLoadedMsg{runs: runs, err: err}
	}
}

// Update implements tea.Model.
func (v *ConfigSyncRunsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		v.height = m.Height
		return v, nil

	case syncRunsLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.runs = m.runs
		}
		return v, nil

	case tea.KeyMsg:
		return v.handleKey(m.String())
	}
	return v, nil
}

func (v *ConfigSyncRunsView) handleKey(key string) (tea.Model, tea.Cmd) {
	switch v.mode {
	case syncRunsModeDetail:
		items := flattenVisible(v.detailNodes, 0)
		switch key {
		case "esc", "q":
			v.mode = syncRunsModeList
		case "j", "down":
			if v.detailCursor < len(items)-1 {
				v.detailCursor++
				v.clampDetailScroll(len(items))
			}
		case "k", "up":
			if v.detailCursor > 0 {
				v.detailCursor--
				v.clampDetailScroll(len(items))
			}
		case "enter", " ":
			if v.detailCursor < len(items) {
				node := items[v.detailCursor].node
				if len(node.children) > 0 {
					node.expanded = !node.expanded
					// Clamp cursor in case visible list shrank.
					newItems := flattenVisible(v.detailNodes, 0)
					if v.detailCursor >= len(newItems) {
						v.detailCursor = len(newItems) - 1
					}
					v.clampDetailScroll(len(newItems))
				}
			}
		}
		return v, nil

	default: // list mode
		switch key {
		case "j", "down":
			if v.cursor < len(v.runs)-1 {
				v.cursor++
				v.clampScroll()
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				v.clampScroll()
			}
		case "enter":
			if v.cursor < len(v.runs) && len(v.runs[v.cursor].Timings) > 0 {
				v.openDetail(v.runs[v.cursor].Timings)
				v.mode = syncRunsModeDetail
			}
		case "r":
			v.loading = true
			v.errMsg = ""
			return v, v.load()
		}
		return v, nil
	}
}

// openDetail initialises the tree for a run's timings.
func (v *ConfigSyncRunsView) openDetail(timings map[string]int64) {
	v.detailNodes = buildTimingTree(timings)
	v.detailCursor = 0
	v.detailScroll = 0
	// Compute global max for bar scaling.
	var maxMS int64
	for _, ms := range timings {
		if ms > maxMS {
			maxMS = ms
		}
	}
	v.detailMaxMS = maxMS
}

// View implements tea.Model.
func (v *ConfigSyncRunsView) View() string {
	if v.mode == syncRunsModeDetail {
		return v.renderDetail()
	}
	return v.renderList()
}

// ── list view ─────────────────────────────────────────────────────────────────

func (v *ConfigSyncRunsView) renderList() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config — Sync Runs") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}

	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.listFooter())
		return sb.String()
	}

	if len(v.runs) == 0 {
		sb.WriteString("  No sync runs found.\n")
		sb.WriteString(v.listFooter())
		return sb.String()
	}

	avail := v.visibleRows()
	start := v.scrollOffset
	end := start + avail
	if end > len(v.runs) {
		end = len(v.runs)
	}

	for i, run := range v.runs[start:end] {
		i += start
		prefix := "  "
		if i == v.cursor {
			prefix = "> "
		}

		scope := run.Scope
		if run.TeamName != nil {
			scope = fmt.Sprintf("team:%s", *run.TeamName)
		} else if run.TeamID != nil {
			scope = fmt.Sprintf("team:%d", *run.TeamID)
		}

		dur := ""
		if run.DurationMs != nil {
			dur = fmtSyncDuration(*run.DurationMs)
		} else if run.Status == "running" {
			if t, err := time.Parse(time.RFC3339Nano, run.StartedAt); err == nil {
				dur = fmtSyncDuration(time.Since(t).Milliseconds()) + "…"
			}
		}

		startedStr := ""
		if t, err := time.Parse(time.RFC3339Nano, run.StartedAt); err == nil {
			startedStr = t.Local().Format("01-02 15:04")
		}

		hint := ""
		if len(run.Timings) > 0 {
			hint = cfgDimStyle.Render(" [enter]")
		}

		line := fmt.Sprintf("%s#%-4d %-22s %s  %-8s %s%s",
			prefix, run.ID, scope, syncStatusStyle(run.Status), dur, startedStr, hint,
		)
		if run.Status == "error" && run.Error != nil {
			line += cfgDimStyle.Render(fmt.Sprintf("  — %s", syncTruncate(*run.Error, 40)))
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString(v.listFooter())
	return sb.String()
}

// ── detail / tree view ────────────────────────────────────────────────────────

func (v *ConfigSyncRunsView) renderDetail() string {
	if v.cursor >= len(v.runs) {
		return ""
	}
	run := v.runs[v.cursor]

	scope := run.Scope
	if run.TeamName != nil {
		scope = fmt.Sprintf("team:%s", *run.TeamName)
	}

	var header strings.Builder
	header.WriteString("\n  " + cfgSelectedStyle.Render(fmt.Sprintf("Config — Sync Run #%d", run.ID)) + "\n\n")
	header.WriteString(fmt.Sprintf("  Scope:  %s\n", scope))
	header.WriteString(fmt.Sprintf("  Status: %s", syncStatusStyle(run.Status)))
	if run.DurationMs != nil {
		header.WriteString(fmt.Sprintf("  ·  total %s", fmtSyncDuration(*run.DurationMs)))
	}
	header.WriteString("\n")
	if run.Error != nil {
		header.WriteString(fmt.Sprintf("  Error:  %s\n", *run.Error))
	}

	footer := "\n" + cfgDimStyle.Render("  j/k move  ·  Enter/Space expand  ·  Esc back") + "\n"

	if len(v.detailNodes) == 0 {
		return header.String() + "\n  No timing data.\n" + footer
	}

	// Flatten currently visible tree items.
	items := flattenVisible(v.detailNodes, 0)

	// Clamp cursor.
	if v.detailCursor >= len(items) {
		v.detailCursor = len(items) - 1
	}

	// Header line count for available-row calculation.
	headerLines := strings.Count(header.String(), "\n") + 2 // +2 for "Timings" label + blank
	avail := v.height - headerLines - 3                      // 3 for footer
	if avail < 3 {
		avail = 3
	}
	v.clampDetailScroll(len(items))

	start := v.detailScroll
	end := start + avail
	if end > len(items) {
		end = len(items)
	}

	const labelW = 30
	const barW = 16

	var sb strings.Builder
	sb.WriteString(header.String())
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Timings") + cfgDimStyle.Render(" (Enter to expand)") + "\n\n")

	for i, it := range items[start:end] {
		absIdx := start + i
		sel := absIdx == v.detailCursor

		indent := strings.Repeat("  ", it.depth)
		prefix := "  "
		if sel {
			prefix = "> "
		}

		// Icon: ▶ collapsed container, ▼ expanded container, · leaf
		icon := "· "
		if len(it.node.children) > 0 {
			if it.node.expanded {
				icon = "▼ "
			} else {
				icon = "▶ "
			}
		}

		// Label: node's own label, truncated to fit column.
		maxLabel := labelW - len(indent)
		if maxLabel < 6 {
			maxLabel = 6
		}
		label := syncTruncate(it.node.label, maxLabel)

		// Duration: own ms if present, else dim for containers.
		var durStr string
		if it.node.ms > 0 {
			durStr = fmt.Sprintf("%7s", fmtSyncDuration(it.node.ms))
		} else if len(it.node.children) > 0 {
			durStr = cfgDimStyle.Render(fmt.Sprintf("%7s", fmt.Sprintf("[%d]", len(it.node.children))))
		} else {
			durStr = fmt.Sprintf("%7s", "—")
		}

		// Bar: scaled to global max; dim for containers without own ms.
		barLen := 0
		if v.detailMaxMS > 0 && it.node.ms > 0 {
			barLen = int(float64(it.node.ms) / float64(v.detailMaxMS) * float64(barW))
		}
		bar := strings.Repeat("█", barLen) + strings.Repeat("░", barW-barLen)
		var barRendered string
		if it.node.ms > 0 {
			barRendered = cfgSelectedStyle.Render(bar)
		} else {
			barRendered = cfgDimStyle.Render(bar)
		}

		// Pad label to fixed width.
		labelPad := fmt.Sprintf("%-*s", maxLabel, label)
		line := fmt.Sprintf("%s%s%s%s  %s  %s",
			prefix, indent, icon, labelPad, durStr, barRendered,
		)
		if sel {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	// Scroll indicator.
	if len(items) > avail {
		shown := end - start
		pct := 0
		if len(items)-avail > 0 {
			pct = v.detailScroll * 100 / (len(items) - avail)
		}
		sb.WriteString(cfgDimStyle.Render(fmt.Sprintf("  — %d–%d of %d  %d%% —",
			start+1, start+shown, len(items), pct)) + "\n")
	}
	sb.WriteString(footer)
	return sb.String()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (v *ConfigSyncRunsView) listFooter() string {
	return "\n" + cfgDimStyle.Render("  j/k navigate  ·  Enter for timings  ·  r refresh  ·  Esc back") + "\n"
}

func (v *ConfigSyncRunsView) visibleRows() int {
	if v.height <= 0 {
		return 20
	}
	return maxInt(5, v.height-8)
}

func (v *ConfigSyncRunsView) clampScroll() {
	avail := v.visibleRows()
	if v.cursor < v.scrollOffset {
		v.scrollOffset = v.cursor
	}
	if v.cursor >= v.scrollOffset+avail {
		v.scrollOffset = v.cursor - avail + 1
	}
}

func (v *ConfigSyncRunsView) clampDetailScroll(total int) {
	avail := v.detailVisibleRows()
	if v.detailCursor < v.detailScroll {
		v.detailScroll = v.detailCursor
	}
	if v.detailCursor >= v.detailScroll+avail {
		v.detailScroll = v.detailCursor - avail + 1
	}
	maxScroll := total - avail
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.detailScroll > maxScroll {
		v.detailScroll = maxScroll
	}
}

func (v *ConfigSyncRunsView) detailVisibleRows() int {
	if v.height <= 0 {
		return 15
	}
	return maxInt(3, v.height-10) // header (~7) + footer (3)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func syncStatusStyle(s string) string {
	switch s {
	case "done":
		return syncGreen.Render(s)
	case "running":
		return syncYellow.Render(s)
	case "error":
		return syncRed.Render(s)
	default:
		return s
	}
}

func fmtSyncDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dm%ds", ms/60000, (ms%60000)/1000)
}

func syncTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

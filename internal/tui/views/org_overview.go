package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/msgs"
	"github.com/your-org/dashboard/internal/tui/views/config"
)

// ---- internal message types ----

type orgLoadedMsg struct {
	data *client.OrgOverviewResponse
	err  error
}

type orgSyncStartedMsg struct {
	runID int64
	err   error
}

type orgSyncPollMsg struct{ runID int64 }

type orgSyncDoneMsg struct {
	status string
	errMsg string
}

type orgCalendarLoadedMsg struct {
	data *client.OrgCalendarResponse
	err  error
}

// ---- styles ----

var (
	riskHighStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	riskMediumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	riskLowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	riskNormalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	dimStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	syncBannerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// calPalette maps team index → terminal color for calendar indicators.
var calPalette = []lipgloss.Color{"14", "11", "13", "10", "9", "6"}

// teamEmojis cycles through emojis for each team card.
var teamEmojis = []string{"🚀", "⚡", "🎯", "🔥", "🌊", "🌟"}

// OrgOverviewView shows all teams at a glance.
type OrgOverviewView struct {
	c       *client.Client
	data    *client.OrgOverviewResponse
	cursor  int
	loading bool
	errMsg  string
	syncing bool
	syncMsg string
	width   int
	height  int

	// calendar
	calendar        *client.OrgCalendarResponse
	calendarLoading bool
	calendarErr     string
	calendarMonth   time.Time // first day of the displayed month
}

// NewOrgOverviewView creates the org overview view.
func NewOrgOverviewView(c *client.Client) *OrgOverviewView {
	now := time.Now()
	return &OrgOverviewView{
		c:               c,
		loading:         true,
		calendarLoading: true,
		calendarMonth:   time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local),
	}
}

// Init implements tea.Model — load org data and calendar in parallel.
func (v *OrgOverviewView) Init() tea.Cmd {
	return tea.Batch(v.loadData(), v.loadCalendar())
}

func (v *OrgOverviewView) loadData() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetOrgOverview()
		return orgLoadedMsg{data: data, err: err}
	}
}

func (v *OrgOverviewView) loadCalendar() tea.Cmd {
	return func() tea.Msg {
		data, err := v.c.GetOrgCalendar("", "") // fetch all events
		return orgCalendarLoadedMsg{data: data, err: err}
	}
}

func doOrgSync(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		runID, err := c.PostSync("org", nil)
		return orgSyncStartedMsg{runID: runID, err: err}
	}
}

func pollOrgSync(c *client.Client, runID int64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return orgSyncDoneMsg{status: "error", errMsg: err.Error()}
		}
		if run.Status == "done" || run.Status == "error" {
			errDetail := ""
			if run.Error != nil {
				errDetail = *run.Error
			}
			return orgSyncDoneMsg{status: run.Status, errMsg: errDetail}
		}
		return orgSyncPollMsg{runID: runID}
	}
}

// Update implements tea.Model.
func (v *OrgOverviewView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = m.Width
		v.height = m.Height
		return v, nil

	case orgLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.data = m.data
			v.errMsg = ""
		}
		return v, nil

	case orgCalendarLoadedMsg:
		v.calendarLoading = false
		if m.err != nil {
			v.calendarErr = m.err.Error()
		} else {
			v.calendar = m.data
			v.calendarErr = ""
		}
		return v, nil

	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			if v.data != nil && v.cursor < len(v.data.Teams)-1 {
				v.cursor++
			}
			return v, nil
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return v, nil
		case "enter":
			if v.data != nil && len(v.data.Teams) > 0 {
				team := v.data.Teams[v.cursor]
				tv := NewTeamView(v.c, team.ID, team.Name)
				return v, func() tea.Msg {
					return msgs.PushViewMsg{View: tv}
				}
			}
			return v, nil
		case "c":
			cv := config.NewConfigRootView(v.c)
			return v, func() tea.Msg { return msgs.PushViewMsg{View: cv} }
		case "R":
			if !v.syncing {
				v.syncing = true
				v.syncMsg = "Syncing org…"
				return v, doOrgSync(v.c)
			}
			return v, nil
		case "[":
			v.calendarMonth = v.calendarMonth.AddDate(0, -1, 0)
			return v, nil
		case "]":
			v.calendarMonth = v.calendarMonth.AddDate(0, 1, 0)
			return v, nil
		}

	case orgSyncStartedMsg:
		if m.err != nil {
			v.syncing = false
			v.syncMsg = ""
			v.errMsg = "Sync failed: " + m.err.Error()
		}
		return v, pollOrgSync(v.c, m.runID)

	case orgSyncPollMsg:
		return v, pollOrgSync(v.c, m.runID)

	case orgSyncDoneMsg:
		v.syncing = false
		if m.status == "error" && m.errMsg != "" {
			v.syncMsg = "Sync error: " + m.errMsg
		} else {
			v.syncMsg = ""
		}
		// Reload both data and calendar after sync.
		v.loading = true
		v.calendarLoading = true
		return v, tea.Batch(v.loadData(), v.loadCalendar())
	}

	return v, nil
}

// panelW returns the content width for bordered panels.
func (v *OrgOverviewView) panelW() int {
	w := v.width - 6
	if w < 60 {
		return 60
	}
	return w
}

// renderHeader returns a full-width dark bar with "Org Overview" and sync/state info.
func (v *OrgOverviewView) renderHeader() string {
	w := v.width
	if w < 60 {
		w = 60
	}
	hBg := lipgloss.Color("17")

	right := ""
	if v.syncing {
		right = "⟳  Syncing…"
	} else if v.syncMsg != "" {
		right = v.syncMsg
	} else if v.data != nil && v.data.LastSyncedAt != nil {
		right = "synced " + *v.data.LastSyncedAt
	}

	title := lipgloss.NewStyle().
		Background(hBg).Foreground(lipgloss.Color("15")).Bold(true).
		Padding(0, 2).Render("🏠 Org Overview")
	rightRendered := lipgloss.NewStyle().
		Background(hBg).Foreground(lipgloss.Color("8")).
		Padding(0, 2).Render(right)

	gap := w - lipgloss.Width(title) - lipgloss.Width(rightRendered)
	if gap < 0 {
		gap = 0
	}
	fill := lipgloss.NewStyle().Background(hBg).Render(strings.Repeat(" ", gap))
	return title + fill + rightRendered
}

// renderTeamCard renders a single team as a bordered panel card.
func (v *OrgOverviewView) renderTeamCard(i int, team client.OrgTeamItem) string {
	selected := i == v.cursor
	borderColor := lipgloss.Color("238")
	if selected {
		borderColor = lipgloss.Color("14") // cyan when selected
	}

	emoji := teamEmojis[i%len(teamEmojis)]
	nameStr := emoji + " " + team.Name
	if selected {
		nameStr = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Render(nameStr)
	} else {
		nameStr = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Render(nameStr)
	}

	sprintStr := fmt.Sprintf("Week %d / %d", team.CurrentSprint, team.TotalSprints)
	if team.TotalSprints > 4 {
		sprintStr = fmt.Sprintf("Week %d / ", team.CurrentSprint) +
			warningAmberStyle.Render(fmt.Sprintf("%d", team.TotalSprints))
	}

	risk := renderRisk(team.RiskLevel)

	focus := team.Focus
	contentW := v.panelW() - 4
	if len(focus) > contentW {
		focus = focus[:contentW-1] + "…"
	}

	var c strings.Builder
	c.WriteString(dimStyle.Render("Sprint") + "  " + sprintStr +
		"   " + dimStyle.Render("Risk") + "  " + risk + "\n")
	if focus != "" {
		c.WriteString("\n" + dimStyle.Render("Focus  ") + focus + "\n")
	}

	body := nameStr + "\n\n" + strings.TrimRight(c.String(), "\n")
	boxed := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(v.panelW()).
		Padding(0, 1).
		Render(body)

	lines := strings.Split(strings.TrimRight(boxed, "\n"), "\n")
	for j := range lines {
		lines[j] = "  " + lines[j]
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// View implements tea.Model.
func (v *OrgOverviewView) View() string {
	var sb strings.Builder

	// ── Header bar ────────────────────────────────────────────────────────
	sb.WriteString("\n")
	sb.WriteString(v.renderHeader())
	sb.WriteString("\n\n")

	// Error
	if v.errMsg != "" {
		sb.WriteString("  " + errorStyle.Render("Error: "+v.errMsg) + "\n\n")
	}

	// Loading
	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	// No data
	if v.data == nil || len(v.data.Teams) == 0 {
		sb.WriteString("  No data yet. Press R to sync.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	// Sync banner (below header when syncing in background)
	if v.syncMsg != "" && !v.syncing {
		sb.WriteString("  " + syncBannerStyle.Render(v.syncMsg) + "\n\n")
	}

	// ── Team cards ────────────────────────────────────────────────────────
	for i, team := range v.data.Teams {
		sb.WriteString(v.renderTeamCard(i, team))
	}

	// ── Calendar ──────────────────────────────────────────────────────────
	{
		var c strings.Builder
		c.WriteString(strings.TrimRight(v.renderCalendarGrid(), "\n"))
		// Render as a panel matching team page style.
		borderColor := lipgloss.Color("238")
		heading := sectionHeading("📅 Calendar")
		body := heading + "\n\n" + strings.TrimRight(c.String(), "\n")
		boxed := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(v.panelW()).
			Padding(0, 1).
			Render(body)
		lines := strings.Split(strings.TrimRight(boxed, "\n"), "\n")
		for i := range lines {
			lines[i] = "  " + lines[i]
		}
		sb.WriteString(strings.Join(lines, "\n") + "\n\n")
	}

	sb.WriteString(v.footer())
	return sb.String()
}

// ── Calendar rendering ────────────────────────────────────────────────────────

// teamCalendarInfo returns the display color and single-letter initial for a team ID.
// Colors cycle through calPalette in team list order.
func (v *OrgOverviewView) teamCalendarInfo(teamID int64) (color lipgloss.Color, initial string) {
	if v.data != nil {
		for i, t := range v.data.Teams {
			if t.ID == teamID {
				col := calPalette[i%len(calPalette)]
				ini := "?"
				if t.Name != "" {
					ini = strings.ToUpper(t.Name[:1])
				}
				return col, ini
			}
		}
	}
	return "7", "?"
}

// renderCalendarGrid renders two consecutive months side by side, then an event list below.
func (v *OrgOverviewView) renderCalendarGrid() string {
	if v.calendarLoading {
		return dimStyle.Render("  Loading…") + "\n"
	}
	if v.calendarErr != "" {
		return errorStyle.Render("  Error: "+v.calendarErr) + "\n"
	}

	month1 := v.calendarMonth
	month2 := v.calendarMonth.AddDate(0, 1, 0)

	// Title line: ◀ Month1 Year    Month2 Year ▶   [ ] months
	name1 := "◀ " + month1.Format("January 2006")
	name2 := month2.Format("January 2006") + " ▶"
	const colWidth = 28
	const gap = "    "
	padTitle := colWidth + len(gap) - lipgloss.Width(name1)
	if padTitle < 1 {
		padTitle = 1
	}

	var sb strings.Builder
	sb.WriteString("  " +
		selectedStyle.Render(name1) +
		strings.Repeat(" ", padTitle) +
		selectedStyle.Render(name2) +
		"  " + dimStyle.Render("[ ] months") + "\n\n")

	// Grid lines for each month (each line is colWidth visible chars)
	lines1 := v.monthGridLines(month1)
	lines2 := v.monthGridLines(month2)

	maxLines := len(lines1)
	if len(lines2) > maxLines {
		maxLines = len(lines2)
	}
	for len(lines1) < maxLines {
		lines1 = append(lines1, "")
	}
	for len(lines2) < maxLines {
		lines2 = append(lines2, "")
	}

	for i := range lines1 {
		l1, l2 := lines1[i], lines2[i]
		pad := colWidth - lipgloss.Width(l1)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString("  " + l1 + strings.Repeat(" ", pad) + gap + l2 + "\n")
	}

	// Team legend
	if v.data != nil && len(v.data.Teams) > 0 {
		sb.WriteString("\n  ")
		for i, t := range v.data.Teams {
			color, initial := v.teamCalendarInfo(t.ID)
			entry := lipgloss.NewStyle().Foreground(color).Render(initial + " " + t.Name)
			if i > 0 {
				sb.WriteString("   ")
			}
			sb.WriteString(entry)
		}
		sb.WriteString("\n")
	}

	// Events for both displayed months combined
	periodStart := month1.Format("2006-01-02")
	periodEnd := month2.AddDate(0, 1, -1).Format("2006-01-02")
	var events []client.OrgCalendarEvent
	if v.calendar != nil {
		for _, e := range v.calendar.Events {
			if e.Date >= periodStart && e.Date <= periodEnd {
				events = append(events, e)
			}
		}
	}
	if len(events) > 0 {
		sb.WriteString("\n")
		for _, e := range events {
			t, _ := time.Parse("2006-01-02", e.Date)
			dateStr := t.Format("Jan 02")
			color, _ := v.teamCalendarInfo(e.TeamID)
			teamStr := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%-14s", e.TeamName))
			confStr := " "
			if e.DateConfidence == "inferred" {
				confStr = dimStyle.Render("~")
			}
			flagStr := ""
			if e.HasFlags {
				flagStr = "  " + warningAmberStyle.Render("⚠")
			}
			sb.WriteString(fmt.Sprintf("  %s%s  %s  %s%s\n",
				confStr, dimStyle.Render(dateStr), teamStr, e.Title, flagStr))
		}
	} else if v.calendar != nil {
		sb.WriteString(dimStyle.Render("  No events in this period.") + "\n")
	}

	return sb.String()
}

// monthGridLines returns the day-of-week header row followed by week rows for a single month.
// Each string has exactly colWidth (28) visible characters: 7 cells × 4 chars each.
func (v *OrgOverviewView) monthGridLines(month time.Time) []string {
	// Header: " Mo  Tu  We  Th  Fr  Sa  Su " — each slot is 4 visible chars.
	lines := []string{dimStyle.Render(" Mo  Tu  We  Th  Fr  Sa  Su ")}

	monthStart := month.Format("2006-01-02")
	monthEnd := month.AddDate(0, 1, -1).Format("2006-01-02")
	dayEvents := map[int][]client.OrgCalendarEvent{}
	if v.calendar != nil {
		for _, e := range v.calendar.Events {
			if e.Date >= monthStart && e.Date <= monthEnd {
				if t, err := time.Parse("2006-01-02", e.Date); err == nil {
					dayEvents[t.Day()] = append(dayEvents[t.Day()], e)
				}
			}
		}
	}

	daysInMonth := month.AddDate(0, 1, -1).Day()
	startOffset := calMondayFirst(month.Weekday())
	now := time.Now()
	isCurrentMonth := month.Year() == now.Year() && month.Month() == now.Month()

	totalCells := startOffset + daysInMonth
	if totalCells%7 != 0 {
		totalCells += 7 - totalCells%7
	}

	for row := 0; row < totalCells/7; row++ {
		var lb strings.Builder
		for col := 0; col < 7; col++ {
			day := row*7 + col - startOffset + 1
			if day < 1 || day > daysInMonth {
				lb.WriteString("    ")
			} else {
				lb.WriteString(v.renderGridCell(day, dayEvents[day], isCurrentMonth && day == now.Day()))
			}
		}
		lines = append(lines, lb.String())
	}
	return lines
}

// renderGridCell returns the 4-char string for a single day cell in the grid.
// Format: " DD" (right-aligned 2-digit day, space-padded) + indicator (1 char).
// indicator is a colored team initial, "+" for multiple teams, or " " for none.
func (v *OrgOverviewView) renderGridCell(day int, events []client.OrgCalendarEvent, isToday bool) string {
	indicator := " "
	if len(events) > 0 {
		// Count distinct teams
		seen := map[int64]bool{}
		for _, e := range events {
			seen[e.TeamID] = true
		}
		if len(seen) == 1 {
			color, initial := v.teamCalendarInfo(events[0].TeamID)
			indicator = lipgloss.NewStyle().Foreground(color).Render(initial)
		} else {
			indicator = lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Render("+")
		}
	}

	dayStr := fmt.Sprintf("%2d", day)
	if isToday {
		dayStr = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Render(fmt.Sprintf("%2d", day))
	}

	return " " + dayStr + indicator
}

// calMondayFirst converts time.Weekday to a Monday-first index (Mon=0, Sun=6).
func calMondayFirst(wd time.Weekday) int {
	if wd == time.Sunday {
		return 6
	}
	return int(wd) - 1
}

// ── Footer / helpers ──────────────────────────────────────────────────────────

func (v *OrgOverviewView) footer() string {
	return "\n" + dimStyle.Render(
		"  j/k navigate  ·  Enter drill in  ·  R sync org  ·  [ ] months  ·  c config  ·  q quit",
	) + "\n"
}

func renderRisk(level string) string {
	switch strings.ToUpper(level) {
	case "HIGH":
		return riskHighStyle.Render("HIGH")
	case "MEDIUM":
		return riskMediumStyle.Render("MEDIUM")
	case "LOW":
		return riskLowStyle.Render("LOW")
	default:
		return riskNormalStyle.Render(level)
	}
}

// PushViewMsg is an alias kept for backward compatibility; use msgs.PushViewMsg directly.
type PushViewMsg = msgs.PushViewMsg

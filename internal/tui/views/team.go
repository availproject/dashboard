package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/your-org/dashboard/internal/tui/client"
)

var (
	noteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// sectionHeading renders a section heading with bold, cyan, and underline applied
// as a single ANSI sequence so the underline covers spaces between words.
func sectionHeading(text string) string {
	p := lipgloss.DefaultRenderer().ColorProfile()
	return p.String(text).Bold().Underline().Foreground(p.Color("14")).String()
}

type teamViewMode int

const (
	teamViewModeScroll   teamViewMode = iota // default: j/k scroll line by line
	teamViewModeAnnotate                     // a: j/k jump by section, enter annotate, esc back
)

// ---- sync / autotag message types ----

type reportSyncStartedMsg struct {
	runID int64
	err   error
}

type reportSyncPollMsg struct{ runID int64 }

type reportSyncDoneMsg struct {
	status string
	errMsg string
}

type autotagStartedMsg struct {
	runID int64
	err   error
}
type autotagPollMsg struct{ runID int64 }
type autotagDoneMsg struct{ err error }

// TeamReportView shows all team sections in a single scrollable page.
type TeamReportView struct {
	c        *client.Client
	teamID   int64
	teamName string

	sprint    *client.SprintResponse
	goals     *client.GoalsResponse
	workload  *client.WorkloadResponse
	velocity  *client.VelocityResponse
	metrics   *client.MetricsResponse
	activity  *client.ActivityResponse
	marketing *client.MarketingResponse
	calendar  *client.CalendarResponse

	sprintLoading    bool
	goalsLoading     bool
	workloadLoading  bool
	velocityLoading  bool
	metricsLoading   bool
	activityLoading  bool
	marketingLoading bool
	calendarLoading  bool

	sprintErr    string
	goalsErr     string
	workloadErr  string
	velocityErr  string
	metricsErr   string
	activityErr  string
	marketingErr string
	calendarErr  string

	mode          teamViewMode
	scrollY       int
	cursorIdx     int
	cursorLines   []int // line index per annotatable item, populated each render
	calendarMode  calendarViewMode
	calendarMonth time.Time // first day of displayed month (grid mode)

	height int
	width  int

	syncing    bool
	autotagging bool
	syncMsg    string
	errMsg     string
}

// NewTeamView creates a TeamReportView for the given team.
func NewTeamView(c *client.Client, teamID int64, name string) *TeamReportView {
	now := time.Now()
	return &TeamReportView{
		c:                c,
		teamID:           teamID,
		teamName:         name,
		sprintLoading:    true,
		goalsLoading:     true,
		workloadLoading:  true,
		velocityLoading:  true,
		metricsLoading:   true,
		activityLoading:  true,
		marketingLoading: true,
		calendarLoading:  true,
		calendarMode:     calendarModeGrid,
		calendarMonth:    time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local),
		height:           40,
	}
}

type activityLoadedMsg struct {
	data *client.ActivityResponse
	err  error
}

type marketingLoadedMsg struct {
	data *client.MarketingResponse
	err  error
}

type calendarLoadedMsg struct {
	data *client.CalendarResponse
	err  error
}

// Init implements tea.Model — load all sections in parallel.
func (v *TeamReportView) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			data, err := v.c.GetSprint(v.teamID)
			return sprintLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetGoals(v.teamID)
			return goalsLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetWorkload(v.teamID)
			return workloadLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetVelocity(v.teamID)
			return velocityLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetMetrics(v.teamID)
			return metricsLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetActivity(v.teamID)
			return activityLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetMarketing(v.teamID)
			return marketingLoadedMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := v.c.GetCalendar(v.teamID, "", "")
			return calendarLoadedMsg{data: data, err: err}
		},
	)
}

func doAutotag(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		runID, err := c.PostAutotag()
		return autotagStartedMsg{runID: runID, err: err}
	}
}

func pollAutotag(c *client.Client, runID int64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(3 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return autotagDoneMsg{err: err}
		}
		if run.Status == "done" || run.Status == "error" {
			var runErr error
			if run.Error != nil {
				runErr = fmt.Errorf("%s", *run.Error)
			}
			return autotagDoneMsg{err: runErr}
		}
		return autotagPollMsg{runID: runID}
	}
}

func doReportSync(c *client.Client, teamID int64) tea.Cmd {
	return func() tea.Msg {
		runID, err := c.PostSync("team", &teamID)
		return reportSyncStartedMsg{runID: runID, err: err}
	}
}

func pollReportSync(c *client.Client, runID int64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		run, err := c.GetSyncRun(runID)
		if err != nil {
			return reportSyncDoneMsg{status: "error", errMsg: err.Error()}
		}
		if run.Status == "done" || run.Status == "error" {
			errDetail := ""
			if run.Error != nil {
				errDetail = *run.Error
			}
			return reportSyncDoneMsg{status: run.Status, errMsg: errDetail}
		}
		return reportSyncPollMsg{runID: runID}
	}
}

func (v *TeamReportView) reload() tea.Cmd {
	v.sprintLoading = true
	v.goalsLoading = true
	v.workloadLoading = true
	v.velocityLoading = true
	v.metricsLoading = true
	v.activityLoading = true
	v.marketingLoading = true
	v.calendarLoading = true
	return v.Init()
}

// Update implements tea.Model.
func (v *TeamReportView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		v.height = m.Height
		v.width = m.Width
		return v, nil

	case sprintLoadedMsg:
		v.sprintLoading = false
		if m.err != nil {
			v.sprintErr = m.err.Error()
		} else {
			v.sprint = m.data
			v.sprintErr = ""
		}
		return v, nil

	case goalsLoadedMsg:
		v.goalsLoading = false
		if m.err != nil {
			v.goalsErr = m.err.Error()
		} else {
			v.goals = m.data
			v.goalsErr = ""
		}
		return v, nil

	case workloadLoadedMsg:
		v.workloadLoading = false
		if m.err != nil {
			v.workloadErr = m.err.Error()
		} else {
			v.workload = m.data
			v.workloadErr = ""
		}
		return v, nil

	case velocityLoadedMsg:
		v.velocityLoading = false
		if m.err != nil {
			v.velocityErr = m.err.Error()
		} else {
			v.velocity = m.data
			v.velocityErr = ""
		}
		return v, nil

	case metricsLoadedMsg:
		v.metricsLoading = false
		if m.err != nil {
			v.metricsErr = m.err.Error()
		} else {
			v.metrics = m.data
			v.metricsErr = ""
		}
		return v, nil

	case activityLoadedMsg:
		v.activityLoading = false
		if m.err != nil {
			v.activityErr = m.err.Error()
		} else {
			v.activity = m.data
			v.activityErr = ""
		}
		return v, nil

	case marketingLoadedMsg:
		v.marketingLoading = false
		if m.err != nil {
			v.marketingErr = m.err.Error()
		} else {
			v.marketing = m.data
			v.marketingErr = ""
		}
		return v, nil

	case calendarLoadedMsg:
		v.calendarLoading = false
		if m.err != nil {
			v.calendarErr = m.err.Error()
		} else {
			v.calendar = m.data
			v.calendarErr = ""
		}
		return v, nil

	case reportSyncStartedMsg:
		if m.err != nil {
			v.syncing = false
			v.syncMsg = ""
			v.errMsg = "Sync failed: " + m.err.Error()
			return v, nil
		}
		return v, pollReportSync(v.c, m.runID)

	case reportSyncPollMsg:
		return v, pollReportSync(v.c, m.runID)

	case reportSyncDoneMsg:
		v.syncing = false
		if m.status == "error" && m.errMsg != "" {
			v.syncMsg = "Sync error: " + m.errMsg
		} else {
			v.syncMsg = "Sync complete."
		}
		return v, v.reload()

	case autotagStartedMsg:
		if m.err != nil {
			v.autotagging = false
			v.syncMsg = "Tag GitHub tasks failed: " + m.err.Error()
			return v, nil
		}
		return v, pollAutotag(v.c, m.runID)

	case autotagPollMsg:
		return v, pollAutotag(v.c, m.runID)

	case autotagDoneMsg:
		v.autotagging = false
		if m.err != nil {
			v.syncMsg = "Tag GitHub tasks failed: " + m.err.Error()
		} else {
			v.syncMsg = "Tagging done — press r to sync."
		}
		return v, nil

	case tea.KeyMsg:
		switch v.mode {
		case teamViewModeScroll:
			switch m.String() {
			case "j", "down":
				v.scrollY++
			case "k", "up":
				if v.scrollY > 0 {
					v.scrollY--
				}
			case "d", "ctrl+d":
				v.scrollY += v.pageSize() / 2
			case "u", "ctrl+u":
				if v.scrollY -= v.pageSize() / 2; v.scrollY < 0 {
					v.scrollY = 0
				}
			case "a":
				v.mode = teamViewModeAnnotate
				v.snapCursorToVisible()
			case "r":
				if !v.syncing && !v.autotagging {
					v.syncing = true
					v.syncMsg = "Syncing team…"
					return v, doReportSync(v.c, v.teamID)
				}
			case "t":
				if !v.autotagging && !v.syncing {
					v.autotagging = true
					v.syncMsg = "Tagging GitHub tasks…"
					return v, doAutotag(v.c)
				}
			case "v":
				if v.calendarMode == calendarModeList {
					v.calendarMode = calendarModeGrid
				} else {
					v.calendarMode = calendarModeList
				}
			case "[":
				if v.calendarMode == calendarModeGrid {
					v.calendarMonth = v.calendarMonth.AddDate(0, -1, 0)
				}
			case "]":
				if v.calendarMode == calendarModeGrid {
					v.calendarMonth = v.calendarMonth.AddDate(0, 1, 0)
				}
			}
			return v, nil

		case teamViewModeAnnotate:
			switch m.String() {
			case "j", "down":
				items := v.annotateItems()
				if v.cursorIdx < len(items)-1 {
					v.cursorIdx++
					v.scrollToCursor()
				}
			case "k", "up":
				if v.cursorIdx > 0 {
					v.cursorIdx--
					v.scrollToCursor()
				}
			case "enter":
				items := v.annotateItems()
				if v.cursorIdx >= 0 && v.cursorIdx < len(items) {
					it := items[v.cursorIdx]
					sectionKey := it.itemRef
					if it.tier == "team" {
						sectionKey = "team"
					}
					var existing []client.SectionAnnotation
					if v.goals != nil {
						existing = v.goals.SectionAnnotations[sectionKey]
					}
					av := NewSectionAnnotateView(v.c, v.teamID, it.tier, it.itemRef, existing)
					return v, func() tea.Msg { return PushViewMsg{View: av} }
				}
			case "esc":
				v.mode = teamViewModeScroll
			}
			return v, nil
		}
	}

	return v, nil
}

// goalStatusBadge returns a styled badge for a business goal status.
func goalStatusBadge(status string) string {
	switch strings.ToLower(status) {
	case "on_track":
		return riskLowStyle.Render("[ON TRACK]")
	case "at_risk":
		return warningAmberStyle.Render("[AT RISK] ")
	case "behind":
		return riskHighStyle.Render("[BEHIND]  ")
	case "unclear":
		return dimStyle.Render("[UNCLEAR] ")
	default:
		return dimStyle.Render("[" + status + "]")
	}
}

// sprintGoalStatusBadge returns a styled badge for a sprint goal status.
func sprintGoalStatusBadge(status string) string {
	switch strings.ToLower(status) {
	case "on_track":
		return riskLowStyle.Render("[ON TRACK]")
	case "at_risk":
		return warningAmberStyle.Render("[AT RISK] ")
	case "unclear":
		return dimStyle.Render("[UNCLEAR] ")
	default:
		return dimStyle.Render("[" + status + "]")
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func wordWrap(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) <= width {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	return append(lines, line)
}

func (v *TeamReportView) pageSize() int {
	ps := v.height - 3
	if ps < 5 {
		return 5
	}
	return ps
}

// annotateItems returns the ordered list of annotatable items matching
// the render order: team annotation, business goals section, sprint goals section, concerns section.
// Each item uses the section key as itemRef so all items in a section share the same annotations.
func (v *TeamReportView) annotateItems() []annotatePickItem {
	items := []annotatePickItem{
		{tier: "team", itemRef: "", label: v.teamName},
	}
	if v.goals != nil {
		if len(v.goals.BusinessGoals) > 0 {
			items = append(items, annotatePickItem{tier: "item", itemRef: "section:business_goals", label: "Business Goals"})
		}
		if len(v.goals.SprintGoals) > 0 {
			items = append(items, annotatePickItem{tier: "item", itemRef: "section:sprint_goals", label: "Sprint Goals"})
		}
		if len(v.goals.Concerns) > 0 {
			items = append(items, annotatePickItem{tier: "item", itemRef: "section:concerns", label: "Concerns"})
		}
	}
	return items
}

// sectionBadge returns a count badge string for a section, e.g. " [2]".
func (v *TeamReportView) sectionBadge(key string) string {
	if v.goals == nil {
		return ""
	}
	anns := v.goals.SectionAnnotations[key]
	if len(anns) == 0 {
		return ""
	}
	return " " + dimStyle.Render(fmt.Sprintf("[%d]", len(anns)))
}

// scrollToCursor adjusts scrollY so the cursored item is visible.
func (v *TeamReportView) scrollToCursor() {
	if v.cursorIdx < 0 || v.cursorIdx >= len(v.cursorLines) {
		return
	}
	line := v.cursorLines[v.cursorIdx]
	if line < v.scrollY {
		v.scrollY = line
	} else if line >= v.scrollY+v.pageSize() {
		v.scrollY = line - v.pageSize() + 1
	}
}

// snapCursorToVisible moves the cursor to the topmost annotatable item
// that is currently visible (at or after scrollY).
func (v *TeamReportView) snapCursorToVisible() {
	for i, line := range v.cursorLines {
		if line >= v.scrollY {
			v.cursorIdx = i
			return
		}
	}
	if len(v.cursorLines) > 0 {
		v.cursorIdx = len(v.cursorLines) - 1
	}
}

// View implements tea.Model.
func (v *TeamReportView) View() string {
	// Clamp cursor in case item count changed since last render.
	if n := len(v.annotateItems()); v.cursorIdx >= n {
		v.cursorIdx = n - 1
	}
	if v.cursorIdx < 0 {
		v.cursorIdx = 0
	}

	content := v.renderContent()
	lines := strings.Split(content, "\n")

	ps := v.pageSize()
	maxScroll := len(lines) - ps
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.scrollY > maxScroll {
		v.scrollY = maxScroll
	}

	end := v.scrollY + ps
	if end > len(lines) {
		end = len(lines)
	}

	visible := strings.Join(lines[v.scrollY:end], "\n")

	scrollIndicator := ""
	if maxScroll > 0 {
		pct := v.scrollY * 100 / maxScroll
		scrollIndicator = "  " + dimStyle.Render(fmt.Sprintf("%d%%", pct))
	}

	var footer string
	switch v.mode {
	case teamViewModeAnnotate:
		footer = "\n" + warningAmberStyle.Render("  ANNOTATE  ") + "  " + dimStyle.Render("j/k section  ·  Enter open  ·  Esc exit") + "\n"
	default:
		footer = "\n" + dimStyle.Render("  j/k scroll  ·  d/u page  ·  a annotate  ·  r sync  ·  t tag  ·  Esc back") + scrollIndicator + "\n"
	}
	return visible + footer
}

func (v *TeamReportView) sprintEndDate() string {
	if v.sprint == nil || v.sprint.StartDate == nil {
		return ""
	}
	t, err := time.Parse("2006-01-02", *v.sprint.StartDate)
	if err != nil {
		return ""
	}
	sprintStart := t.AddDate(0, 0, (v.sprint.CurrentSprint-1)*7)
	sprintEnd := sprintStart.AddDate(0, 0, 6)
	return sprintEnd.Format("Jan 2")
}

func (v *TeamReportView) wrapWidth() int {
	w := v.width - 8
	if w < 60 {
		return 60
	}
	if w > 120 {
		return 120
	}
	return w
}

func (v *TeamReportView) renderContent() string {
	var sb strings.Builder

	// Cursor tracking: record the line number of each annotatable item as we render.
	newCursorLines := make([]int, 0, 20)
	annotateIdx := 0
	markLine := func() {
		newCursorLines = append(newCursorLines, strings.Count(sb.String(), "\n"))
	}
	cursorMark := func() string {
		idx := annotateIdx
		annotateIdx++
		if v.mode == teamViewModeAnnotate && idx == v.cursorIdx {
			return "> "
		}
		return "  "
	}

	sb.WriteString("\n  " + selectedStyle.Render(v.teamName) + "\n")

	if v.mode == teamViewModeAnnotate {
		markLine()
		sb.WriteString(cursorMark() + dimStyle.Render("[Team annotation]") + v.sectionBadge("team") + "\n")
	} else {
		// still need to advance annotateIdx so cursorIdx stays aligned
		annotateIdx++
		newCursorLines = append(newCursorLines, strings.Count(sb.String(), "\n"))
	}

	if v.syncMsg != "" {
		style := syncBannerStyle
		if strings.HasPrefix(v.syncMsg, "Sync error") {
			style = errorStyle
		}
		sb.WriteString(style.Render("  "+v.syncMsg) + "\n")
	}
	if v.errMsg != "" {
		sb.WriteString(errorStyle.Render("  "+v.errMsg) + "\n")
	}

	sb.WriteString("\n")

	// ── Business Goals ────────────────────────────────────────────────────
	{
		hasItems := v.goals != nil && len(v.goals.BusinessGoals) > 0
		bizCursor := "  "
		if hasItems {
			markLine()
			bizCursor = cursorMark()
		}
		sb.WriteString(bizCursor + sectionHeading("Business Goals") + v.sectionBadge("section:business_goals") + "\n\n")
		if v.goalsLoading {
			sb.WriteString(dimStyle.Render("  Loading…") + "\n")
		} else if v.goalsErr != "" {
			sb.WriteString(errorStyle.Render("  Error: "+v.goalsErr) + "\n")
		} else if !hasItems {
			sb.WriteString(dimStyle.Render("  No data. Press r to sync.") + "\n")
		} else {
			for _, g := range v.goals.BusinessGoals {
				badge := goalStatusBadge(g.Status)
				sb.WriteString("  " + badge + " " + g.Text + "\n")
				if g.Note != "" {
					for _, line := range wordWrap(g.Note, v.wrapWidth()) {
						sb.WriteString("    " + noteStyle.Render(line) + "\n")
					}
				}
			}
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Calendar ───────────────────────────────────────────────────────────
	{
		modeLabel := "v list"
		if v.calendarMode == calendarModeList {
			modeLabel = "v grid"
		}
		sb.WriteString("  " + sectionHeading("Calendar") +
			"  " + dimStyle.Render(modeLabel) + "\n\n")
		if v.calendarMode == calendarModeGrid {
			sb.WriteString(v.renderTeamCalendarGrid())
		} else {
			v.renderTeamCalendarList(&sb)
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Sprint Status ─────────────────────────────────────────────────────
	{
		hasSprintGoals := v.goals != nil && len(v.goals.SprintGoals) > 0
		sprintCursor := "  "
		if hasSprintGoals {
			markLine()
			sprintCursor = cursorMark()
		}
		sb.WriteString(sprintCursor + sectionHeading("Sprint Status") + v.sectionBadge("section:sprint_goals") + "\n\n")
		if v.sprintLoading || v.goalsLoading {
			sb.WriteString(dimStyle.Render("  Loading…") + "\n")
		} else if v.sprintErr != "" {
			sb.WriteString(errorStyle.Render("  Error: "+v.sprintErr) + "\n")
		} else {
			// Sprint header line
			if v.sprint != nil {
				totalStr := fmt.Sprintf("%d", v.sprint.TotalSprints)
				if v.sprint.TotalSprints > 4 {
					totalStr = warningAmberStyle.Render(totalStr)
				}
				header := fmt.Sprintf("  Week %d of %s", v.sprint.CurrentSprint, totalStr)
				if end := v.sprintEndDate(); end != "" {
					header += dimStyle.Render(" · ends "+end)
				}
				if v.sprint.PlanTitle != "" {
					planStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Underline(true)
					header += dimStyle.Render("  ·  ") + planStyle.Render(v.sprint.PlanTitle)
				}
				sb.WriteString(header + "\n")
				if v.sprint.StartDateMissing {
					sb.WriteString(warningAmberStyle.Render("  ⚠  Sprint start date not found. Add it to the plan doc or annotate it in Config.") + "\n")
				}
				if v.sprint.NextPlanStartRisk {
					sb.WriteString(errorStyle.Render(fmt.Sprintf("  ✗  Plan extended to sprint %d — delays next plan start.", v.sprint.TotalSprints)) + "\n")
				}
			}

			// Sprint goals with status
			if v.goals != nil {
				if len(v.goals.SprintGoals) == 0 {
					sb.WriteString(dimStyle.Render("\n  (no sprint goals)") + "\n")
				} else {
					sb.WriteString("\n")
					for _, g := range v.goals.SprintGoals {
						badge := sprintGoalStatusBadge(g.Status)
						sb.WriteString("  " + badge + " " + g.Text + "\n")
						if g.Note != "" {
							for _, line := range wordWrap(g.Note, v.wrapWidth()) {
								sb.WriteString("    " + noteStyle.Render(line) + "\n")
							}
						}
					}
				}

				// Forecast paragraph
				if v.goals.SprintForecast != "" {
					sb.WriteString("\n")
					for _, line := range wordWrap(v.goals.SprintForecast, v.wrapWidth()) {
						sb.WriteString("  " + noteStyle.Render(line) + "\n")
					}
				}
			}
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Marketing ─────────────────────────────────────────────────────────
	sb.WriteString("  " + sectionHeading("Marketing") + "\n\n")
	if v.marketingLoading {
		sb.WriteString(dimStyle.Render("  Loading…") + "\n")
	} else if v.marketingErr != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.marketingErr) + "\n")
	} else if v.marketing == nil || len(v.marketing.Campaigns) == 0 {
		sb.WriteString(dimStyle.Render("  No campaigns. Configure a marketing_calendar source in team config.") + "\n")
	} else {
		for i, camp := range v.marketing.Campaigns {
			if i > 0 {
				sb.WriteString("\n")
			}
			// Campaign header
			statusCol := lipgloss.Color("245")
			if camp.Status == "In Progress" {
				statusCol = lipgloss.Color("14")
			}
			statusBadge := lipgloss.NewStyle().Foreground(statusCol).Render(camp.Status)
			dateRange := ""
			if camp.DateStart != nil && camp.DateEnd != nil {
				dateRange = "  " + dimStyle.Render(*camp.DateStart+" – "+*camp.DateEnd)
			}
			sb.WriteString("  " + selectedStyle.Render(camp.Title) + "  " + statusBadge + dateRange + "\n")

			// Tasks
			for _, task := range camp.Tasks {
				bullet := "  ○ "
				if task.Status == "In Progress" {
					bullet = "  ● "
				} else if task.Status == "Done" || task.Status == "Complete" {
					bullet = "  ✓ "
				}
				taskStatus := dimStyle.Render(fmt.Sprintf("%-14s", task.Status))
				assignee := ""
				if task.Assignee != "" {
					assignee = "  " + dimStyle.Render(task.Assignee)
				}
				sb.WriteString(fmt.Sprintf("  %s%s  %s%s\n", bullet, truncate(task.Title, 36), taskStatus, assignee))
			}
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Engineering ───────────────────────────────────────────────────────
	sb.WriteString("  " + sectionHeading("Engineering") + "\n\n")
	if v.activityLoading {
		sb.WriteString(dimStyle.Render("  Loading…") + "\n")
	} else if v.activityErr != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.activityErr) + "\n")
	} else if v.activity == nil {
		sb.WriteString(dimStyle.Render("  No data. Press r to sync.") + "\n")
	} else {
		a := v.activity
		// Pre-filter issues: exclude terminal project statuses for current-sprint view.
		var activeIssues []client.ActivityIssue
		for _, iss := range a.OpenIssues {
			switch iss.ProjectStatus {
			case "Done", "Not Completed", "Not Complete", "Won't Do":
				// skip
			default:
				activeIssues = append(activeIssues, iss)
			}
		}
		// Summary line
		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			dimStyle.Render(fmt.Sprintf("%d commits", len(a.RecentCommits))),
			dimStyle.Render(fmt.Sprintf("%d PRs merged", len(a.MergedPRs))),
			dimStyle.Render(fmt.Sprintf("%d open issues", len(activeIssues))),
		))
		sb.WriteString("\n")

		// Recent commits
		if len(a.RecentCommits) > 0 {
			sb.WriteString("  " + noteStyle.Render("Recent Commits") + "\n")
			for _, c := range a.RecentCommits {
				sha := dimStyle.Render(c.SHA[:7])
				author := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(fmt.Sprintf("%-10s", c.Author))
				repo := dimStyle.Render("[" + c.Repo + "]")
				msg := c.Message
				if len(msg) > v.wrapWidth()-30 {
					msg = msg[:v.wrapWidth()-33] + "…"
				}
				sb.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n", sha, author, msg, repo))
			}
			sb.WriteString("\n")
		}

		// Open issues (current sprint — exclude terminal statuses)
		if len(activeIssues) > 0 {
			sb.WriteString("  " + noteStyle.Render("Open Issues (Current sprint)") + "\n")
			for _, iss := range activeIssues {
				statusStr := ""
				if iss.ProjectStatus != "" {
					col := lipgloss.Color("245")
					switch iss.ProjectStatus {
					case "In Progress":
						col = lipgloss.Color("14")
					case "In Review":
						col = lipgloss.Color("12")
					}
					statusStr = lipgloss.NewStyle().Foreground(col).Render(fmt.Sprintf("%-12s", iss.ProjectStatus))
				}
				assignee := ""
				if iss.Assignee != "" {
					assignee = dimStyle.Render("@" + iss.Assignee)
				}
				sb.WriteString(fmt.Sprintf("  #%-5d  %s  %-38s  %s\n",
					iss.Number, statusStr, truncate(iss.Title, 38), assignee))
			}
			sb.WriteString("\n")
		}

		// Merged PRs
		if len(a.MergedPRs) > 0 {
			sb.WriteString("  " + noteStyle.Render("Merged PRs") + "\n")
			for _, pr := range a.MergedPRs {
				author := dimStyle.Render("@" + pr.Author)
				sb.WriteString(fmt.Sprintf("  #%-5d  %-42s  %s  %s\n",
					pr.Number, truncate(pr.Title, 42), author, dimStyle.Render(pr.MergedAt)))
			}
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Concerns ──────────────────────────────────────────────────────────
	{
		hasConcerns := v.goals != nil && len(v.goals.Concerns) > 0
		concernsCursor := "  "
		if hasConcerns {
			markLine()
			concernsCursor = cursorMark()
		}
		sb.WriteString(concernsCursor + sectionHeading("Concerns") + v.sectionBadge("section:concerns") + "\n\n")
		if v.goalsLoading {
			sb.WriteString(dimStyle.Render("  Loading…") + "\n")
		} else if v.goalsErr != "" {
			sb.WriteString(errorStyle.Render("  Error: "+v.goalsErr) + "\n")
		} else if !hasConcerns {
			sb.WriteString(dimStyle.Render("  (no concerns)") + "\n")
		} else {
			for _, c := range v.goals.Concerns {
				var severityStr string
				if strings.HasPrefix(c.Key, "stale_annotation_") {
					severityStr = warningAmberStyle.Render("[STALE]")
				} else {
					switch strings.ToUpper(c.Severity) {
					case "HIGH":
						severityStr = riskHighStyle.Render("[HIGH]  ")
					case "MEDIUM":
						severityStr = riskMediumStyle.Render("[MEDIUM]")
					case "LOW":
						severityStr = dimStyle.Render("[LOW]   ")
					default:
						severityStr = "[" + c.Severity + "]"
					}
				}
				scopeStr := ""
				switch strings.ToLower(c.Scope) {
				case "strategic":
					scopeStr = " " + dimStyle.Render("[STRATEGIC]")
				case "sprint":
					scopeStr = " " + dimStyle.Render("[SPRINT]   ")
				}
				sb.WriteString("  " + severityStr + scopeStr + " " + c.Summary + "\n")
				if c.Explanation != "" {
					for _, line := range wordWrap(c.Explanation, v.wrapWidth()) {
						sb.WriteString("    " + noteStyle.Render(line) + "\n")
					}
				}
			}
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Velocity ──────────────────────────────────────────────────────────
	sb.WriteString("  " + sectionHeading("Velocity") + "\n\n")
	if v.velocityLoading {
		sb.WriteString(dimStyle.Render("  Loading…") + "\n")
	} else if v.velocityErr != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.velocityErr) + "\n")
	} else if v.velocity == nil || len(v.velocity.Sprints) == 0 {
		sb.WriteString(dimStyle.Render("  No data. Press r to sync.") + "\n")
	} else {
		sb.WriteString("  " + selectedStyle.Render(sparkline(v.velocity.Sprints)) + "\n\n")
		sb.WriteString(fmt.Sprintf("  %-16s  %6s  %8s  %6s  %7s\n", "Sprint", "Score", "Issues", "PRs", "Commits"))
		sb.WriteString(dimStyle.Render("  "+strings.Repeat("─", 52)) + "\n")
		for _, sp := range v.velocity.Sprints {
			sb.WriteString(fmt.Sprintf("  %-16s  %6.1f  %8.0f  %6.0f  %7.0f\n",
				sp.Label, sp.Score, sp.Breakdown.Issues, sp.Breakdown.PRs, sp.Breakdown.Commits))
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Resource / Workload ───────────────────────────────────────────────
	sb.WriteString("  " + sectionHeading("Resource / Workload") + "\n\n")
	if v.workloadLoading {
		sb.WriteString(dimStyle.Render("  Loading…") + "\n")
	} else if v.workloadErr != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.workloadErr) + "\n")
	} else if v.workload == nil || len(v.workload.Members) == 0 {
		sb.WriteString(dimStyle.Render("  No data. Press r to sync.") + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("  %-24s  %-10s  %s\n", "Member", "Est. Days", "Load"))
		sb.WriteString(dimStyle.Render("  "+strings.Repeat("─", 46)) + "\n")
		for _, m := range v.workload.Members {
			labelStyle := riskNormalStyle
			switch strings.ToUpper(m.Label) {
			case "HIGH":
				labelStyle = riskHighStyle
			case "LOW":
				labelStyle = dimStyle
			}
			label := labelStyle.Render(fmt.Sprintf("[%s]", strings.ToUpper(m.Label)))
			sb.WriteString(fmt.Sprintf("  %-24s  %-10s  %s\n", m.Name, fmt.Sprintf("%.1f days", m.EstimatedDays), label))
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  "+strings.Repeat("─", 60)) + "\n\n")

	// ── Business Metrics ──────────────────────────────────────────────────
	sb.WriteString("  " + sectionHeading("Business Metrics") + "\n\n")
	if v.metricsLoading {
		sb.WriteString(dimStyle.Render("  Loading…") + "\n")
	} else if v.metricsErr != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.metricsErr) + "\n")
	} else if v.metrics == nil || len(v.metrics.Panels) == 0 {
		sb.WriteString(dimStyle.Render("  No data. Press r to sync.") + "\n")
	} else {
		for _, p := range v.metrics.Panels {
			value := dimStyle.Render("—")
			if p.Value != nil {
				value = *p.Value
			}
			sb.WriteString("  " + selectedStyle.Render(p.Title) + "  " + value + "\n")
		}
	}

	sb.WriteString("\n")
	v.cursorLines = newCursorLines
	return sb.String()
}

// renderTeamCalendarList writes the list-style calendar to sb.
func (v *TeamReportView) renderTeamCalendarList(sb *strings.Builder) {
	if v.calendarLoading {
		sb.WriteString(dimStyle.Render("  Loading…") + "\n")
		return
	}
	if v.calendarErr != "" {
		sb.WriteString(errorStyle.Render("  Error: "+v.calendarErr) + "\n")
		return
	}
	if v.calendar == nil || (len(v.calendar.Events) == 0 && len(v.calendar.Undated) == 0) {
		sb.WriteString(dimStyle.Render("  No calendar data. Press r to sync.") + "\n")
		return
	}
	if len(v.calendar.Events) > 0 {
		for _, e := range v.calendar.Events {
			sb.WriteString(v.renderCalendarEvent(e))
		}
	}
	if len(v.calendar.Undated) > 0 {
		sb.WriteString("\n  " + warningAmberStyle.Render("Needs Date") + "\n")
		for _, e := range v.calendar.Undated {
			label := warningAmberStyle.Render("[NEEDS DATE]")
			typeStr := dimStyle.Render(calendarEventTypeLabel(e.EventType))
			sb.WriteString(fmt.Sprintf("  %s  %s  %s\n", label, typeStr, e.Title))
		}
	}
}

// renderTeamCalendarGrid renders two consecutive months side by side for a single team.
func (v *TeamReportView) renderTeamCalendarGrid() string {
	if v.calendarLoading {
		return dimStyle.Render("  Loading…") + "\n"
	}
	if v.calendarErr != "" {
		return errorStyle.Render("  Error: "+v.calendarErr) + "\n"
	}

	month1 := v.calendarMonth
	month2 := v.calendarMonth.AddDate(0, 1, 0)

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

	lines1 := v.teamMonthGridLines(month1)
	lines2 := v.teamMonthGridLines(month2)

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

	// Events for both displayed months
	periodStart := month1.Format("2006-01-02")
	periodEnd := month2.AddDate(0, 1, -1).Format("2006-01-02")
	var events []client.CalendarEventItem
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
			sb.WriteString(v.renderCalendarEvent(e))
		}
	} else if v.calendar != nil {
		sb.WriteString(dimStyle.Render("  No events in this period.") + "\n")
	}

	if v.calendar != nil && len(v.calendar.Undated) > 0 {
		sb.WriteString("\n  " + warningAmberStyle.Render("Needs Date") + "\n")
		for _, e := range v.calendar.Undated {
			label := warningAmberStyle.Render("[NEEDS DATE]")
			typeStr := dimStyle.Render(calendarEventTypeLabel(e.EventType))
			sb.WriteString(fmt.Sprintf("  %s  %s  %s\n", label, typeStr, e.Title))
		}
	}

	return sb.String()
}

// teamMonthGridLines returns header + week rows for one month, each 28 visible chars.
// Indicators are colored by event type: R=release, D=deadline, M=milestone, C=campaign, s=sprint.
func (v *TeamReportView) teamMonthGridLines(month time.Time) []string {
	lines := []string{dimStyle.Render(" Mo  Tu  We  Th  Fr  Sa  Su ")}

	monthStart := month.Format("2006-01-02")
	monthEnd := month.AddDate(0, 1, -1).Format("2006-01-02")
	dayEvents := map[int][]client.CalendarEventItem{}
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
				lb.WriteString(v.teamGridCell(day, dayEvents[day], isCurrentMonth && day == now.Day()))
			}
		}
		lines = append(lines, lb.String())
	}
	return lines
}

// teamGridCell returns the 4-char cell string for a team calendar day.
// Indicator letter is colored by event type priority.
func (v *TeamReportView) teamGridCell(day int, events []client.CalendarEventItem, isToday bool) string {
	indicator := " "
	if len(events) > 0 {
		// Pick the most prominent event type
		best := events[0]
		priority := func(et string) int {
			switch et {
			case "release":
				return 5
			case "deadline":
				return 4
			case "milestone":
				return 3
			case "campaign_start", "campaign_end":
				return 2
			case "sprint_start", "sprint_end":
				return 1
			default:
				return 0
			}
		}
		for _, e := range events[1:] {
			if priority(e.EventType) > priority(best.EventType) {
				best = e
			}
		}
		var col lipgloss.Color
		var letter string
		switch best.EventType {
		case "release":
			col, letter = "10", "R"
		case "deadline":
			col, letter = "214", "D"
		case "milestone":
			col, letter = "14", "M"
		case "campaign_start", "campaign_end":
			col, letter = "13", "C"
		case "sprint_start", "sprint_end":
			col, letter = "8", "s"
		default:
			col, letter = "7", "·"
		}
		if len(events) > 1 {
			letter = "+"
			col = "7"
		}
		indicator = lipgloss.NewStyle().Foreground(col).Render(letter)
	}

	dayStr := fmt.Sprintf("%2d", day)
	if isToday {
		dayStr = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Render(fmt.Sprintf("%2d", day))
	}
	return " " + dayStr + indicator
}

// renderCalendarEvent renders a single dated calendar event row.
func (v *TeamReportView) renderCalendarEvent(e client.CalendarEventItem) string {
	// Date column (always present for dated events)
	dateStr := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(fmt.Sprintf("%-12s", e.Date))

	// Type label
	typeStr := dimStyle.Render(fmt.Sprintf("%-12s", calendarEventTypeLabel(e.EventType)))

	// Confidence indicator for inferred dates
	confStr := ""
	if e.DateConfidence == "inferred" {
		confStr = dimStyle.Render("~")
	} else {
		confStr = " "
	}

	// Flag indicator
	flagStr := ""
	if hasCalendarFlags(e) {
		flagStr = " " + warningAmberStyle.Render("⚠")
	}

	// Style title by event type
	var titleStr string
	switch e.EventType {
	case "release":
		titleStr = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render(e.Title)
	case "sprint_start", "sprint_end":
		titleStr = dimStyle.Render(e.Title)
	default:
		titleStr = e.Title
	}

	return fmt.Sprintf("  %s%s  %s  %s%s\n", confStr, dateStr, typeStr, titleStr, flagStr)
}

// calendarEventTypeLabel returns a short display label for an event_type.
func calendarEventTypeLabel(t string) string {
	switch t {
	case "release":
		return "Release"
	case "milestone":
		return "Milestone"
	case "deadline":
		return "Deadline"
	case "sprint_start":
		return "Sprint start"
	case "sprint_end":
		return "Sprint end"
	case "campaign_start":
		return "Campaign"
	case "campaign_end":
		return "Campaign end"
	default:
		return t
	}
}

// hasCalendarFlags returns true if the event has any flags embedded in its Flags field.
func hasCalendarFlags(e client.CalendarEventItem) bool {
	if e.Flags == nil {
		return false
	}
	flags, ok := e.Flags.([]any)
	return ok && len(flags) > 0
}

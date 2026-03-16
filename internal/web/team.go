package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ---- API response types ----

type teamSprintResp struct {
	PlanType          string   `json:"plan_type"`
	PlanTitle         string   `json:"plan_title"`
	PlanURL           string   `json:"plan_url"`
	StartDate         *string  `json:"start_date"`
	CurrentSprint     int      `json:"current_sprint"`
	TotalSprints      int      `json:"total_sprints"`
	StartDateMissing  bool     `json:"start_date_missing"`
	NextPlanStartRisk bool     `json:"next_plan_start_risk"`
	Goals             []string `json:"goals"`
	LastSyncedAt      *string  `json:"last_synced_at"`
}

type teamBusinessGoal struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type teamSprintGoal struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type teamConcern struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Explanation string `json:"explanation"`
	Severity    string `json:"severity"`
	Scope       string `json:"scope"`
}

type teamSectionAnnotation struct {
	ID      int64  `json:"id"`
	Content string `json:"content"`
}

type teamGoalsResp struct {
	BusinessGoals      []teamBusinessGoal                 `json:"business_goals"`
	SprintGoals        []teamSprintGoal                   `json:"sprint_goals"`
	SprintForecast     string                             `json:"sprint_forecast"`
	Concerns           []teamConcern                      `json:"concerns"`
	SectionAnnotations map[string][]teamSectionAnnotation `json:"section_annotations"`
	LastSyncedAt       *string                            `json:"last_synced_at"`
}

type teamWorkloadMember struct {
	Name          string  `json:"name"`
	EstimatedDays float64 `json:"estimated_days"`
	Label         string  `json:"label"`
}

type teamWorkloadResp struct {
	Members      []teamWorkloadMember `json:"members"`
	LastSyncedAt *string              `json:"last_synced_at"`
}

type velocityBreakdown struct {
	Issues  float64 `json:"issues"`
	PRs     float64 `json:"prs"`
	Commits float64 `json:"commits"`
}

type velocitySprint struct {
	Label     string            `json:"label"`
	Score     float64           `json:"score"`
	Breakdown velocityBreakdown `json:"breakdown"`
}

type teamVelocityResp struct {
	Sprints      []velocitySprint `json:"sprints"`
	LastSyncedAt *string          `json:"last_synced_at"`
}

type teamMetricsPanel struct {
	Title   string  `json:"title"`
	Value   *string `json:"value"`
	PanelID string  `json:"panel_id"`
}

type teamMetricsResp struct {
	Panels       []teamMetricsPanel `json:"panels"`
	LastSyncedAt *string            `json:"last_synced_at"`
}

type activityCommit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Message string `json:"message"`
	Repo    string `json:"repo"`
	Date    string `json:"date"`
}

type activityIssue struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	Assignee      string `json:"assignee"`
	ProjectStatus string `json:"project_status"`
}

type activityPR struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	MergedAt string `json:"merged_at"`
}

type teamActivityResp struct {
	RecentCommits []activityCommit `json:"recent_commits"`
	OpenIssues    []activityIssue  `json:"open_issues"`
	MergedPRs     []activityPR     `json:"merged_prs"`
	LastSyncedAt  *string          `json:"last_synced_at"`
}

type marketingTask struct {
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

type marketingCampaign struct {
	Title     string          `json:"title"`
	Status    string          `json:"status"`
	DateStart *string         `json:"date_start"`
	DateEnd   *string         `json:"date_end"`
	Tasks     []marketingTask `json:"tasks"`
}

type teamMarketingResp struct {
	Campaigns    []marketingCampaign `json:"campaigns"`
	LastSyncedAt *string             `json:"last_synced_at"`
}

type teamCalendarEvent struct {
	EventKey       string `json:"event_key"`
	Title          string `json:"title"`
	EventType      string `json:"event_type"`
	SourceClass    string `json:"source_class"`
	Date           string `json:"date"`
	DateConfidence string `json:"date_confidence"`
	EndDate        string `json:"end_date"`
	NeedsDate      bool   `json:"needs_date"`
	HasFlags       bool   `json:"has_flags"`
}

type teamCalendarResp struct {
	Events  []teamCalendarEvent `json:"events"`
	Undated []teamCalendarEvent `json:"undated"`
}

// ---- Team calendar grid (reuses same logic as org, different event type) ----

type teamCalDay struct {
	Day     int
	IsToday bool
	Events  []teamCalendarEvent
}

type teamCalWeek struct {
	Days [7]*teamCalDay
}

type teamCalMonth struct {
	Label string
	Weeks []teamCalWeek
}

func buildTeamCalMonths(events []teamCalendarEvent, base time.Time) [2]teamCalMonth {
	byDate := map[string][]teamCalendarEvent{}
	for _, e := range events {
		if e.Date != "" {
			byDate[e.Date] = append(byDate[e.Date], e)
		}
	}
	today := time.Now().Format("2006-01-02")

	var out [2]teamCalMonth
	for mi := 0; mi < 2; mi++ {
		m := base.AddDate(0, mi, 0)
		first := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.Local)
		days := first.AddDate(0, 1, -1).Day()
		offset := mondayFirst(first.Weekday())

		out[mi].Label = first.Format("January 2006")

		total := offset + days
		if total%7 != 0 {
			total += 7 - total%7
		}
		for row := 0; row < total/7; row++ {
			var week teamCalWeek
			for col := 0; col < 7; col++ {
				day := row*7 + col - offset + 1
				if day < 1 || day > days {
					continue
				}
				ds := fmt.Sprintf("%04d-%02d-%02d", m.Year(), int(m.Month()), day)
				week.Days[col] = &teamCalDay{
					Day:     day,
					IsToday: ds == today,
					Events:  byDate[ds],
				}
			}
			out[mi].Weeks = append(out[mi].Weeks, week)
		}
	}
	return out
}

// ---- Velocity max score helper ----

func velocityMax(sprints []velocitySprint) float64 {
	max := 0.0
	for _, s := range sprints {
		if s.Score > max {
			max = s.Score
		}
	}
	if max == 0 {
		return 1
	}
	return max
}

// ---- Page handler ----

type teamDashboardPage struct {
	pageBase
	TeamID      int64
	TeamName    string
	Sprint      *teamSprintResp
	Goals       *teamGoalsResp
	Workload    *teamWorkloadResp
	Velocity    *teamVelocityResp
	VelocityMax float64
	Metrics     *teamMetricsResp
	Activity    *teamActivityResp
	Marketing   *teamMarketingResp
	Calendar    *teamCalendarResp
	CalMonths   [2]teamCalMonth
	CalOffset   int
	CalPrev     int
	CalNext     int
}

func (d *Deps) teamDashboard(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}

	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, teamID)

	// Find team name from nav teams.
	teamName := ""
	for _, t := range base.Teams {
		if t.ID == teamID {
			teamName = t.Name
			break
		}
	}

	monthOffset, _ := strconv.Atoi(r.URL.Query().Get("mo"))
	now := time.Now()
	calBase := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, monthOffset, 0)

	prefix := fmt.Sprintf("/teams/%d", teamID)

	var sprint teamSprintResp
	var goals teamGoalsResp
	var workload teamWorkloadResp
	var velocity teamVelocityResp
	var metrics teamMetricsResp
	var activity teamActivityResp
	var marketing teamMarketingResp
	var calendar teamCalendarResp

	// Fetch all sections (errors result in empty structs — render gracefully).
	_ = c.getJSON(prefix+"/sprint", &sprint)
	_ = c.getJSON(prefix+"/goals", &goals)
	_ = c.getJSON(prefix+"/workload", &workload)
	_ = c.getJSON(prefix+"/velocity", &velocity)
	_ = c.getJSON(prefix+"/metrics", &metrics)
	_ = c.getJSON(prefix+"/activity", &activity)
	_ = c.getJSON(prefix+"/marketing", &marketing)
	_ = c.getJSON(prefix+"/calendar", &calendar)

	calMonths := buildTeamCalMonths(calendar.Events, calBase)

	render(w, "team.html", teamDashboardPage{
		pageBase:    base,
		TeamID:      teamID,
		TeamName:    teamName,
		Sprint:      &sprint,
		Goals:       &goals,
		Workload:    &workload,
		Velocity:    &velocity,
		VelocityMax: velocityMax(velocity.Sprints),
		Metrics:     &metrics,
		Activity:    &activity,
		Marketing:   &marketing,
		Calendar:    &calendar,
		CalMonths:   calMonths,
		CalOffset:   monthOffset,
		CalPrev:     monthOffset - 1,
		CalNext:     monthOffset + 1,
	})
}

// ---- Annotation handlers ----

type annotationFormData struct {
	TeamID  int64
	Section string
}

func (d *Deps) annotationNewForm(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	teamID, _ := strconv.ParseInt(chi_urlparam(r, "teamID"), 10, 64)
	section := r.URL.Query().Get("section")
	renderPartial(w, "annotation_form.html", annotationFormData{TeamID: teamID, Section: section})
}

func (d *Deps) createAnnotation(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	teamID, _ := strconv.ParseInt(r.FormValue("team_id"), 10, 64)
	section := r.FormValue("section")
	content := r.FormValue("content")
	tier := r.FormValue("tier")
	if tier == "" {
		tier = "team"
	}

	c := newAPIClient(r, d.APIBase)
	payload := map[string]any{
		"tier":    tier,
		"team_id": teamID,
		"content": content,
	}
	if section != "" && section != "team" {
		payload["item_ref"] = section
	}
	var created struct {
		ID      int64  `json:"id"`
		Content string `json:"content"`
	}
	if err := c.postJSON("/annotations", payload, &created); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated annotation list for this section.
	var goals teamGoalsResp
	_ = c.getJSON(fmt.Sprintf("/teams/%d/goals", teamID), &goals)
	renderPartial(w, "annotation_list.html", annotationListData{
		TeamID:      teamID,
		Section:     section,
		Annotations: goals.SectionAnnotations[sectionKey(section)],
		IsEdit:      ctxRole_(r) == "edit",
	})
}

func (d *Deps) updateAnnotation(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	annID, err := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	content := r.FormValue("content")
	c := newAPIClient(r, d.APIBase)
	if err := c.putJSON(fmt.Sprintf("/annotations/%d", annID), map[string]string{"content": content}, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) deleteAnnotation(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	annID, err := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.deleteJSON(fmt.Sprintf("/annotations/%d", annID)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Return empty string — htmx will replace the element with nothing.
	w.WriteHeader(http.StatusOK)
}

type annotationListData struct {
	TeamID      int64
	Section     string
	Annotations []teamSectionAnnotation
	IsEdit      bool
}

func sectionKey(section string) string {
	if section == "" || section == "team" {
		return "team"
	}
	return section
}

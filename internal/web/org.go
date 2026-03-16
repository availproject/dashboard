package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ---- API response types ----

type orgTeamItem struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	CurrentSprint int     `json:"current_sprint"`
	TotalSprints  int     `json:"total_sprints"`
	RiskLevel     string  `json:"risk_level"`
	Focus         string  `json:"focus"`
	LastSyncedAt  *string `json:"last_synced_at"`
}

type orgWorkloadItem struct {
	Name      string             `json:"name"`
	TotalDays float64            `json:"total_days"`
	Label     string             `json:"label"`
	Breakdown map[string]float64 `json:"breakdown"`
}

type alignmentGoal struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type alignmentResult struct {
	Summary string          `json:"summary"`
	Goals   []alignmentGoal `json:"goals"`
}

type orgOverviewResp struct {
	Teams         []orgTeamItem    `json:"teams"`
	Workload      []orgWorkloadItem `json:"workload"`
	GoalAlignment *alignmentResult  `json:"goal_alignment"`
	LastSyncedAt  *string           `json:"last_synced_at"`
}

type orgCalendarEvent struct {
	TeamID         int64  `json:"team_id"`
	TeamName       string `json:"team_name"`
	Date           string `json:"date"`
	EndDate        string `json:"end_date"`
	Title          string `json:"title"`
	EventType      string `json:"event_type"`
	DateConfidence string `json:"date_confidence"`
	HasFlags       bool   `json:"has_flags"`
	NeedsDate      bool   `json:"needs_date"`
}

type orgCalendarResp struct {
	Events  []orgCalendarEvent `json:"events"`
	Undated []orgCalendarEvent `json:"undated"`
}

// ---- Calendar grid helpers ----

type calDay struct {
	Day     int
	IsToday bool
	Events  []orgCalendarEvent
}

type calWeek struct {
	Days [7]*calDay // Mon=0 … Sun=6; nil = not in month
}

type calMonth struct {
	Label string
	Weeks []calWeek
}

func buildCalMonths(events []orgCalendarEvent, base time.Time) [2]calMonth {
	// Index events by date string.
	byDate := map[string][]orgCalendarEvent{}
	for _, e := range events {
		if e.Date != "" {
			byDate[e.Date] = append(byDate[e.Date], e)
		}
	}
	today := time.Now().Format("2006-01-02")

	var out [2]calMonth
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
			var week calWeek
			for col := 0; col < 7; col++ {
				day := row*7 + col - offset + 1
				if day < 1 || day > days {
					continue
				}
				ds := fmt.Sprintf("%04d-%02d-%02d", m.Year(), int(m.Month()), day)
				week.Days[col] = &calDay{
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

func mondayFirst(wd time.Weekday) int {
	if wd == time.Sunday {
		return 6
	}
	return int(wd) - 1
}

// ---- Page handler ----

type orgOverviewPage struct {
	pageBase
	Overview    *orgOverviewResp
	Calendar    *orgCalendarResp
	Months      [2]calMonth
	CalOffset   int
	CalPrev     int
	CalNext     int
}

func (d *Deps) orgOverview(w http.ResponseWriter, r *http.Request) {
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)

	// Parse calendar month offset from query param.
	monthOffset, _ := strconv.Atoi(r.URL.Query().Get("mo"))
	now := time.Now()
	calBase := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, monthOffset, 0)

	var overview orgOverviewResp
	var calendar orgCalendarResp

	_ = c.getJSON("/org/overview", &overview)
	_ = c.getJSON("/org/calendar", &calendar)

	months := buildCalMonths(calendar.Events, calBase)

	render(w, "org.html", orgOverviewPage{
		pageBase:  base,
		Overview:  &overview,
		Calendar:  &calendar,
		Months:    months,
		CalOffset: monthOffset,
		CalPrev:   monthOffset - 1,
		CalNext:   monthOffset + 1,
	})
}

func (d *Deps) postSync(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	c := newAPIClient(r, d.APIBase)
	var resp struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	if err := c.postJSON("/sync", map[string]any{"scope": "org"}, &resp); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: err.Error()})
		return
	}
	renderPartial(w, "sync_status.html", syncStatusData{RunID: resp.SyncRunID, Polling: true, Scope: "org"})
}

func (d *Deps) postTeamSync(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	teamID, err := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}
	c := newAPIClient(r, d.APIBase)
	var resp struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	if err := c.postJSON("/sync", map[string]any{"scope": "team", "team_id": teamID}, &resp); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: err.Error()})
		return
	}
	renderPartial(w, "sync_status.html", syncStatusData{RunID: resp.SyncRunID, Polling: true, Scope: "team", TeamID: teamID})
}

type syncStatusData struct {
	RunID   int64
	Polling bool
	Status  string
	Error   string
	Scope   string
	TeamID  int64
}

func (d *Deps) syncStatus(w http.ResponseWriter, r *http.Request) {
	runID, err := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c := newAPIClient(r, d.APIBase)
	var run struct {
		ID     int64   `json:"ID"`
		Status string  `json:"Status"`
		Scope  string  `json:"Scope"`
		Error  *string `json:"Error"`
	}
	if err := c.getJSON(fmt.Sprintf("/sync/%d", runID), &run); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: err.Error()})
		return
	}

	errMsg := ""
	if run.Error != nil {
		errMsg = *run.Error
	}

	renderPartial(w, "sync_status.html", syncStatusData{
		RunID:   runID,
		Polling: run.Status != "done" && run.Status != "error",
		Status:  run.Status,
		Error:   errMsg,
		Scope:   run.Scope,
	})
}

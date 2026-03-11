package api

import (
	"encoding/json"
	"net/http"
)

// calendarEventItem is the wire representation of a single calendar event.
type calendarEventItem struct {
	EventKey       string `json:"event_key"`
	Title          string `json:"title"`
	EventType      string `json:"event_type"`
	SourceClass    string `json:"source_class"`
	Date           string `json:"date,omitempty"`
	DateConfidence string `json:"date_confidence"`
	EndDate        string `json:"end_date,omitempty"`
	Sources        any    `json:"sources,omitempty"`  // parsed JSON or nil
	Flags          any    `json:"flags,omitempty"`    // parsed JSON or nil
	NeedsDate      bool   `json:"needs_date"`
}

// calendarResponse is the response body for GET /teams/{id}/calendar.
type calendarResponse struct {
	Events  []calendarEventItem `json:"events"`
	Undated []calendarEventItem `json:"undated"`
}

// ---- Org-level calendar ----

type orgCalendarEventItem struct {
	TeamID         int64  `json:"team_id"`
	TeamName       string `json:"team_name"`
	Date           string `json:"date,omitempty"`
	EndDate        string `json:"end_date,omitempty"`
	Title          string `json:"title"`
	EventType      string `json:"event_type"`
	DateConfidence string `json:"date_confidence"`
	HasFlags       bool   `json:"has_flags"`
	NeedsDate      bool   `json:"needs_date"`
}

type orgCalendarResponse struct {
	Events  []orgCalendarEventItem `json:"events"`
	Undated []orgCalendarEventItem `json:"undated"`
}

// handleGetOrgCalendar handles GET /org/calendar.
// Optional query params: from, to (YYYY-MM-DD). When omitted, all dated events are returned.
func (d *Deps) handleGetOrgCalendar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	rows, err := d.Store.ListOrgCalendarEvents(ctx, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list org calendar events: "+err.Error())
		return
	}

	resp := orgCalendarResponse{
		Events:  []orgCalendarEventItem{},
		Undated: []orgCalendarEventItem{},
	}
	for _, row := range rows {
		item := orgCalendarEventItem{
			TeamID:         row.TeamID,
			TeamName:       row.TeamName,
			Title:          row.Title,
			EventType:      row.EventType,
			DateConfidence: row.DateConfidence,
			NeedsDate:      row.NeedsDate == 1,
		}
		if row.Date.Valid {
			item.Date = row.Date.String
		}
		if row.EndDate.Valid {
			item.EndDate = row.EndDate.String
		}
		// HasFlags: flags JSON is non-empty and not a bare empty array.
		if row.Flags.Valid && len(row.Flags.String) > 2 {
			item.HasFlags = true
		}
		if row.NeedsDate == 1 {
			resp.Undated = append(resp.Undated, item)
		} else {
			resp.Events = append(resp.Events, item)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetTeamCalendar handles GET /teams/{id}/calendar.
// Optional query params: from, to (YYYY-MM-DD). When omitted, all dated events are returned.
func (d *Deps) handleGetTeamCalendar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	teamID, err := parseTeamID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	rows, err := d.Store.ListCalendarEvents(ctx, teamID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list calendar events: "+err.Error())
		return
	}

	resp := calendarResponse{
		Events:  []calendarEventItem{},
		Undated: []calendarEventItem{},
	}

	for _, e := range rows {
		item := calendarEventItem{
			EventKey:       e.EventKey,
			Title:          e.Title,
			EventType:      e.EventType,
			SourceClass:    e.SourceClass,
			DateConfidence: e.DateConfidence,
			NeedsDate:      e.NeedsDate == 1,
		}
		if e.Date.Valid {
			item.Date = e.Date.String
		}
		if e.EndDate.Valid {
			item.EndDate = e.EndDate.String
		}
		if e.Sources.Valid && e.Sources.String != "" {
			var parsed any
			if err := json.Unmarshal([]byte(e.Sources.String), &parsed); err == nil {
				item.Sources = parsed
			}
		}
		if e.Flags.Valid && e.Flags.String != "" {
			var parsed any
			if err := json.Unmarshal([]byte(e.Flags.String), &parsed); err == nil {
				item.Flags = parsed
			}
		}

		if e.NeedsDate == 1 {
			resp.Undated = append(resp.Undated, item)
		} else {
			resp.Events = append(resp.Events, item)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

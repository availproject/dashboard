package web

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strings"
	"time"
)

// riskClass maps a risk level string to a CSS class.
func riskClass(level string) string {
	switch strings.ToUpper(level) {
	case "HIGH":
		return "badge-red"
	case "MEDIUM":
		return "badge-amber"
	case "LOW":
		return "badge-green"
	default:
		return "badge-muted"
	}
}

// workloadClass maps a workload label to a CSS class.
func workloadClass(label string) string {
	switch strings.ToUpper(label) {
	case "HIGH":
		return "badge-red"
	case "NORMAL":
		return "badge-cyan"
	case "LOW":
		return "badge-muted"
	default:
		return "badge-muted"
	}
}

// statusIcon returns an icon for a goal/sprint status string.
func statusIcon(status string) string {
	switch strings.ToLower(status) {
	case "on_track", "on track", "done", "completed":
		return "✓"
	case "at_risk", "at risk", "behind":
		return "~"
	case "off_track", "off track", "blocked":
		return "✗"
	default:
		return "·"
	}
}

// statusClass returns a CSS class for a status string.
func statusClass(status string) string {
	switch strings.ToLower(status) {
	case "on_track", "on track", "done", "completed":
		return "status-green"
	case "at_risk", "at risk", "behind":
		return "status-amber"
	case "off_track", "off track", "blocked":
		return "status-red"
	default:
		return "status-muted"
	}
}

// severityClass returns a CSS class for concern severity.
func severityClass(severity string) string {
	switch strings.ToLower(severity) {
	case "high":
		return "severity-high"
	case "medium":
		return "severity-medium"
	case "low":
		return "severity-low"
	default:
		return "severity-low"
	}
}

// pct computes v/max as a percentage (0–100), clamped.
func pct(v, max float64) float64 {
	if max <= 0 {
		return 0
	}
	p := (v / max) * 100
	if p > 100 {
		return 100
	}
	return math.Round(p)
}

// truncate clips s to n runes, adding "…" if clipped.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// sprintDayBar returns a Mon–Fri bar with today highlighted yellow, past days cyan, future dim.
func sprintDayBar() template.HTML {
	wd := int(time.Now().Weekday()) // Sun=0, Mon=1…Fri=5, Sat=6
	if wd == 0 || wd > 5 {
		wd = 5
	}
	var sb strings.Builder
	sb.WriteString(`<span class="sprint-day-bar">[`)
	for i := 1; i <= 5; i++ {
		switch {
		case i == wd:
			sb.WriteString(`<span class="sprint-day-today">█</span>`)
		case i < wd:
			sb.WriteString(`<span class="sprint-day-past">█</span>`)
		default:
			sb.WriteString(`<span class="sprint-day-future">░</span>`)
		}
	}
	sb.WriteString(`]</span>`)
	return template.HTML(sb.String())
}

// sprintPips returns ●/◉/○ pips for sprint position within a plan.
func sprintPips(current, total int) template.HTML {
	if total <= 0 {
		return ""
	}
	var sb strings.Builder
	for i := 1; i <= total; i++ {
		if i > 1 {
			sb.WriteString(` `)
		}
		switch {
		case i < current:
			sb.WriteString(`<span class="sprint-pip-past">●</span>`)
		case i == current:
			sb.WriteString(`<span class="sprint-pip-current">◉</span>`)
		default:
			sb.WriteString(`<span class="sprint-pip-future">○</span>`)
		}
	}
	return template.HTML(sb.String())
}

// sprintBar returns an HTML snippet of filled/empty blocks for sprint position.
func sprintBar(current, total int) template.HTML {
	if total <= 0 {
		return ""
	}
	if current > total {
		current = total
	}
	var sb strings.Builder
	for i := 1; i <= total; i++ {
		if i <= current {
			sb.WriteString(`<span class="sprint-block sprint-block-filled">█</span>`)
		} else {
			sb.WriteString(`<span class="sprint-block sprint-block-empty">░</span>`)
		}
	}
	return template.HTML(sb.String())
}

// navTeam is a minimal team representation for navigation.
type navTeam struct {
	ID   int64
	Name string
}

// pageBase is the common data embedded in every full-page template payload.
type pageBase struct {
	Username     string
	IsEdit       bool
	Teams        []navTeam
	ActiveTeamID int64
	Flash        string
	FlashType    string // "success" | "error"
}

// apiTeamItem is used when unmarshalling the /api/teams list.
type apiTeamItem struct {
	ID             int64        `json:"id"`
	Name           string       `json:"name"`
	MarketingLabel *string      `json:"marketing_label,omitempty"`
	Members        []apiMember  `json:"members"`
}

type apiMember struct {
	ID             int64   `json:"id"`
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

// buildBase fetches teams and builds the common page header data.
func buildBase(r *http.Request, c *apiClient, activeTeamID int64) pageBase {
	var teams []apiTeamItem
	_ = c.getJSON("/teams", &teams)

	nav := make([]navTeam, len(teams))
	for i, t := range teams {
		nav[i] = navTeam{ID: t.ID, Name: t.Name}
	}
	return pageBase{
		Username:     ctxUsername_(r),
		IsEdit:       ctxRole_(r) == "edit",
		Teams:        nav,
		ActiveTeamID: activeTeamID,
	}
}

// flashError writes an htmx-friendly error message as a small HTML fragment.
func flashError(w interface{ Write([]byte) (int, error) }, msg string) {
	fmt.Fprintf(w, `<div class="flash flash-error">%s</div>`, msg)
}

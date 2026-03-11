package sync

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	notionconn "github.com/your-org/dashboard/internal/connector/notion"
	"github.com/your-org/dashboard/internal/pipeline"
	"github.com/your-org/dashboard/internal/store"
)

// writeStructuralCalendarEvents derives sprint boundary and campaign calendar
// events from already-stored data (sprint_meta + marketing snapshot) and writes
// them as source_class='structural' rows, replacing any previous structural
// events for the team.
func (e *Engine) writeStructuralCalendarEvents(ctx context.Context, teamID int64, campaigns []notionconn.MarketingCampaign) error {
	var events []store.CalendarEvent

	// Sprint boundary events from current + next sprint_meta.
	for _, planType := range []string{"current", "next"} {
		meta, err := e.store.GetSprintMeta(ctx, teamID, planType)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			log.Printf("WARN  calendar [team %d]: get sprint_meta %s: %v", teamID, planType, err)
			continue
		}
		prefix := "sprint"
		if planType == "next" {
			prefix = "next-sprint"
		}

		if meta.StartDate.Valid && meta.StartDate.String != "" {
			events = append(events, store.CalendarEvent{
				EventKey:       fmt.Sprintf("%s-start", prefix),
				Title:          sprintEventTitle(meta, planType, "start"),
				EventType:      "sprint_start",
				Date:           meta.StartDate,
				DateConfidence: "confirmed",
			})
		}
		if meta.EndDate.Valid && meta.EndDate.String != "" {
			events = append(events, store.CalendarEvent{
				EventKey:       fmt.Sprintf("%s-end", prefix),
				Title:          sprintEventTitle(meta, planType, "end"),
				EventType:      "sprint_end",
				Date:           meta.EndDate,
				DateConfidence: "confirmed",
			})
		}
	}

	// Campaign events from marketing data.
	for _, c := range campaigns {
		key := slugify(c.Title)
		if c.DateStart != nil {
			events = append(events, store.CalendarEvent{
				EventKey:       fmt.Sprintf("campaign-start-%s", key),
				Title:          fmt.Sprintf("%s — start", c.Title),
				EventType:      "campaign_start",
				Date:           sql.NullString{String: c.DateStart.Format("2006-01-02"), Valid: true},
				DateConfidence: "confirmed",
			})
		}
		if c.DateEnd != nil {
			events = append(events, store.CalendarEvent{
				EventKey:       fmt.Sprintf("campaign-end-%s", key),
				Title:          fmt.Sprintf("%s — end", c.Title),
				EventType:      "campaign_end",
				Date:           sql.NullString{String: c.DateEnd.Format("2006-01-02"), Valid: true},
				DateConfidence: "confirmed",
			})
		}
	}

	if err := e.store.ReplaceCalendarEvents(ctx, teamID, "structural", events); err != nil {
		return fmt.Errorf("write structural calendar events: %w", err)
	}
	log.Printf("INFO  calendar [team %d]: wrote %d structural event(s)", teamID, len(events))
	return nil
}

// sprintEventTitle builds a human-readable title for a sprint boundary event.
func sprintEventTitle(meta *store.SprintMeta, planType, boundary string) string {
	label := "Sprint"
	if meta.SprintNumber.Valid {
		label = fmt.Sprintf("Sprint %d", meta.SprintNumber.Int64)
	}
	if planType == "next" {
		label = "Next " + label
	}
	if boundary == "start" {
		return label + " starts"
	}
	return label + " ends"
}

// slugify returns a URL-safe lowercase slug from s, suitable for use as part
// of an event_key. Non-alphanumeric characters are replaced with hyphens.
func slugify(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			out = append(out, c)
		} else if c >= 'A' && c <= 'Z' {
			out = append(out, c+32)
		} else {
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	// Trim trailing hyphen.
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// buildSprintCalendar constructs a sprint calendar from sprint_meta for the
// team. Uses start_date from current sprint_meta plus sprint_number to derive
// the plan start, then projects out all sprint weeks.
func buildSprintCalendar(meta *store.SprintMeta, totalSprints int) []pipeline.SprintCalEntry {
	if meta == nil || !meta.StartDate.Valid || meta.StartDate.String == "" {
		return nil
	}
	start, err := time.Parse("2006-01-02", meta.StartDate.String)
	if err != nil {
		return nil
	}

	// Back-calculate the plan start: current sprint start minus (sprint_number-1) weeks.
	currentSprint := 1
	if meta.SprintNumber.Valid && meta.SprintNumber.Int64 > 0 {
		currentSprint = int(meta.SprintNumber.Int64)
	}
	planStart := start.AddDate(0, 0, -(currentSprint-1)*7)

	if totalSprints <= 0 {
		totalSprints = 4 // default assumption
	}

	entries := make([]pipeline.SprintCalEntry, 0, totalSprints)
	for i := 0; i < totalSprints; i++ {
		wkStart := planStart.AddDate(0, 0, i*7)
		wkEnd := wkStart.AddDate(0, 0, 4) // Friday
		entries = append(entries, pipeline.SprintCalEntry{
			SprintNum: i + 1,
			StartDate: wkStart.Format("2006-01-02"),
			EndDate:   wkEnd.Format("2006-01-02"),
		})
	}
	return entries
}

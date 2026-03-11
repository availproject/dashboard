package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ReplaceCalendarEvents deletes all existing events for (team_id, source_class)
// and inserts the provided slice in their place. Runs in a single transaction.
func (s *Store) ReplaceCalendarEvents(ctx context.Context, teamID int64, sourceClass string, events []CalendarEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace calendar events: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM calendar_events WHERE team_id = ? AND source_class = ?`,
		teamID, sourceClass,
	); err != nil {
		return fmt.Errorf("replace calendar events: delete: %w", err)
	}

	for _, e := range events {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO calendar_events
    (team_id, event_key, title, event_type, source_class, date, date_confidence,
     end_date, sources, flags, needs_date, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			teamID, e.EventKey, e.Title, e.EventType, sourceClass,
			e.Date, e.DateConfidence, e.EndDate, e.Sources, e.Flags, e.NeedsDate,
		); err != nil {
			return fmt.Errorf("replace calendar events: insert %q: %w", e.EventKey, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace calendar events: commit: %w", err)
	}
	return nil
}

// OrgCalendarRow is a CalendarEvent enriched with the team name, for org-wide queries.
type OrgCalendarRow struct {
	CalendarEvent
	TeamName string
}

// ListOrgCalendarEvents returns calendar events across all teams joined with team name.
// Dated events are optionally filtered to [from, to] (inclusive, YYYY-MM-DD) when both are
// non-empty; when omitted all dated events are returned. Undated events are always included.
// Results are ordered: dated first (asc by date, team name, title), then undated.
func (s *Store) ListOrgCalendarEvents(ctx context.Context, from, to string) ([]OrgCalendarRow, error) {
	var (
		rows *sql.Rows
		err  error
	)

	const selectCols = `
SELECT ce.id, ce.team_id, ce.event_key, ce.title, ce.event_type, ce.source_class,
       ce.date, ce.date_confidence, ce.end_date, ce.sources, ce.flags, ce.needs_date, ce.updated_at,
       t.name
FROM calendar_events ce
JOIN teams t ON t.id = ce.team_id`

	const orderBy = `
ORDER BY CASE WHEN ce.date IS NULL THEN 1 ELSE 0 END, ce.date ASC, t.name ASC, ce.title ASC`

	if from != "" && to != "" {
		rows, err = s.db.QueryContext(ctx,
			selectCols+`
WHERE ce.needs_date = 1
   OR (ce.date IS NOT NULL AND ce.date >= ? AND ce.date <= ?)`+orderBy,
			from, to,
		)
	} else {
		rows, err = s.db.QueryContext(ctx, selectCols+orderBy)
	}
	if err != nil {
		return nil, fmt.Errorf("list org calendar events: %w", err)
	}
	defer rows.Close()

	var result []OrgCalendarRow
	for rows.Next() {
		var row OrgCalendarRow
		var updStr string
		if err := rows.Scan(
			&row.ID, &row.TeamID, &row.EventKey, &row.Title, &row.EventType, &row.SourceClass,
			&row.Date, &row.DateConfidence, &row.EndDate, &row.Sources, &row.Flags,
			&row.NeedsDate, &updStr, &row.TeamName,
		); err != nil {
			return nil, fmt.Errorf("list org calendar events: scan: %w", err)
		}
		row.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updStr)
		result = append(result, row)
	}
	return result, rows.Err()
}

// ListCalendarEvents returns dated events within [from, to] (inclusive, YYYY-MM-DD)
// plus all undated events (needs_date=1) for the team, ordered by date asc then title.
// Empty from/to strings disable the date filter — all dated events are returned.
func (s *Store) ListCalendarEvents(ctx context.Context, teamID int64, from, to string) ([]CalendarEvent, error) {
	var rows *sql.Rows
	var err error

	if from != "" && to != "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, team_id, event_key, title, event_type, source_class, date, date_confidence,
       end_date, sources, flags, needs_date, updated_at
FROM calendar_events
WHERE team_id = ?
  AND (needs_date = 1 OR (date IS NOT NULL AND date >= ? AND date <= ?))
ORDER BY CASE WHEN date IS NULL THEN 1 ELSE 0 END, date ASC, title ASC`,
			teamID, from, to,
		)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, team_id, event_key, title, event_type, source_class, date, date_confidence,
       end_date, sources, flags, needs_date, updated_at
FROM calendar_events
WHERE team_id = ?
ORDER BY CASE WHEN date IS NULL THEN 1 ELSE 0 END, date ASC, title ASC`,
			teamID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list calendar events: %w", err)
	}
	defer rows.Close()

	var events []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		var updStr string
		if err := rows.Scan(
			&e.ID, &e.TeamID, &e.EventKey, &e.Title, &e.EventType, &e.SourceClass,
			&e.Date, &e.DateConfidence, &e.EndDate, &e.Sources, &e.Flags,
			&e.NeedsDate, &updStr,
		); err != nil {
			return nil, fmt.Errorf("list calendar events: scan: %w", err)
		}
		e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

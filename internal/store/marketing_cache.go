package store

import (
	"context"
	"database/sql"
	"fmt"
)

// MarketingPageCache stores the last-fetched state of a Notion task page
// linked from a marketing campaign. Used to skip re-fetching block content
// when the page hasn't changed since the previous sync.
type MarketingPageCache struct {
	PageID     string
	LastEdited string
	Title      string
	Status     string
	Assignee   string
	Body       string
}

// GetMarketingPageCache returns the cached state for the given Notion page ID,
// or nil if not yet cached.
func (s *Store) GetMarketingPageCache(ctx context.Context, pageID string) (*MarketingPageCache, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT page_id, last_edited, title, status, assignee, body
		   FROM marketing_page_cache WHERE page_id = ?`, pageID)
	var c MarketingPageCache
	if err := row.Scan(&c.PageID, &c.LastEdited, &c.Title, &c.Status, &c.Assignee, &c.Body); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get marketing page cache %s: %w", pageID, err)
	}
	return &c, nil
}

// UpsertMarketingPageCache inserts or updates the cached state for a task page.
func (s *Store) UpsertMarketingPageCache(ctx context.Context, c MarketingPageCache) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO marketing_page_cache (page_id, last_edited, title, status, assignee, body, fetched_at)
		     VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(page_id) DO UPDATE SET
		     last_edited = excluded.last_edited,
		     title       = excluded.title,
		     status      = excluded.status,
		     assignee    = excluded.assignee,
		     body        = excluded.body,
		     fetched_at  = excluded.fetched_at`,
		c.PageID, c.LastEdited, c.Title, c.Status, c.Assignee, c.Body,
	)
	if err != nil {
		return fmt.Errorf("upsert marketing page cache %s: %w", c.PageID, err)
	}
	return nil
}

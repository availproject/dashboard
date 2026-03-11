CREATE TABLE IF NOT EXISTS calendar_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id         INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    event_key       TEXT NOT NULL,        -- normalized slug, e.g. "private-alpha-release"
    title           TEXT NOT NULL,
    event_type      TEXT NOT NULL,        -- 'sprint_start' | 'sprint_end' | 'release' | 'milestone' | 'deadline' | 'campaign_start' | 'campaign_end'
    source_class    TEXT NOT NULL,        -- 'structural' | 'synthesized'
    date            TEXT,                 -- YYYY-MM-DD; NULL if undated
    date_confidence TEXT NOT NULL DEFAULT 'confirmed',  -- 'confirmed' | 'inferred' | 'none'
    end_date        TEXT,                 -- YYYY-MM-DD; for multi-day events
    sources         TEXT,                 -- JSON array of per-source attribution (synthesized only)
    flags           TEXT,                 -- JSON array of contradiction/misalignment flags
    needs_date      INTEGER NOT NULL DEFAULT 0,  -- 1 = milestone exists but no date in any source
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(team_id, event_key)
);

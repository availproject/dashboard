CREATE TABLE IF NOT EXISTS team_snapshots (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id      INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    snapshot_type TEXT NOT NULL,   -- 'activity' | 'marketing'
    data         TEXT NOT NULL,    -- JSON blob matching the API response shape
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(team_id, snapshot_type)
);

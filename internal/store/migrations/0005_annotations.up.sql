CREATE TABLE IF NOT EXISTS annotations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    item_ref   TEXT,
    tier       TEXT    NOT NULL CHECK(tier IN ('item', 'team')),
    content    TEXT    NOT NULL,
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

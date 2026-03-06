CREATE TABLE IF NOT EXISTS ai_cache (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    input_hash TEXT    NOT NULL,
    pipeline   TEXT    NOT NULL CHECK(pipeline IN ('sprint_parse', 'concerns', 'goal_extraction', 'workload', 'velocity', 'alignment', 'discovery_suggestion')),
    team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    output     TEXT    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(input_hash, pipeline, team_id)
);

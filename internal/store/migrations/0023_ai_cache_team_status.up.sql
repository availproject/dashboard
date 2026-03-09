-- Add 'team_status' to ai_cache pipeline CHECK constraint.
CREATE TABLE ai_cache_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    input_hash TEXT    NOT NULL,
    pipeline   TEXT    NOT NULL CHECK(pipeline IN ('sprint_parse', 'concerns', 'goal_extraction', 'workload', 'velocity', 'alignment', 'discovery_suggestion', 'label_match', 'homepage_extract', 'team_status')),
    team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    output     TEXT    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(input_hash, pipeline, team_id)
);

INSERT INTO ai_cache_new SELECT * FROM ai_cache;
DROP TABLE ai_cache;
ALTER TABLE ai_cache_new RENAME TO ai_cache;

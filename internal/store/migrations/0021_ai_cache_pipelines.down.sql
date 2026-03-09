-- Revert ai_cache pipeline CHECK to pre-0021 list.
CREATE TABLE ai_cache_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    input_hash TEXT    NOT NULL,
    pipeline   TEXT    NOT NULL CHECK(pipeline IN ('sprint_parse', 'concerns', 'goal_extraction', 'workload', 'velocity', 'alignment', 'discovery_suggestion')),
    team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    output     TEXT    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(input_hash, pipeline, team_id)
);

INSERT INTO ai_cache_new SELECT * FROM ai_cache;
DROP TABLE ai_cache;
ALTER TABLE ai_cache_new RENAME TO ai_cache;

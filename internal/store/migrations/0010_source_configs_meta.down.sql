-- SQLite does not support DROP COLUMN; recreate without config_meta
CREATE TABLE source_configs_new (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    catalogue_id INTEGER NOT NULL REFERENCES source_catalogue(id) ON DELETE CASCADE,
    team_id      INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    purpose      TEXT    NOT NULL CHECK(purpose IN ('current_plan', 'next_plan', 'goals', 'metrics_panel', 'org_goals', 'org_milestones')),
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(catalogue_id, team_id, purpose)
);
INSERT INTO source_configs_new SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs;
DROP TABLE source_configs;
ALTER TABLE source_configs_new RENAME TO source_configs;

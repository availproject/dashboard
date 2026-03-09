CREATE TABLE source_configs_old (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    catalogue_id  INTEGER NOT NULL REFERENCES source_catalogue(id) ON DELETE CASCADE,
    team_id       INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    purpose       TEXT    NOT NULL CHECK(purpose IN ('current_plan', 'next_plan', 'goals', 'metrics_panel', 'org_goals', 'org_milestones', 'task_label')),
    config_meta   TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(catalogue_id, team_id, purpose)
);
INSERT INTO source_configs_old(id,catalogue_id,team_id,purpose,config_meta,created_at) SELECT id,catalogue_id,team_id,purpose,config_meta,created_at FROM source_configs;
DROP TABLE source_configs;
ALTER TABLE source_configs_old RENAME TO source_configs;

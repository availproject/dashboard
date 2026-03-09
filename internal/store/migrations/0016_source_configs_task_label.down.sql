CREATE TABLE source_configs_old (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    catalogue_id  INTEGER NOT NULL REFERENCES source_catalogue(id) ON DELETE CASCADE,
    team_id       INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    purpose       TEXT    NOT NULL CHECK(purpose IN ('current_plan', 'next_plan', 'goals', 'metrics_panel', 'org_goals', 'org_milestones')),
    config_meta   TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO source_configs_old SELECT * FROM source_configs WHERE purpose != 'task_label';
DROP TABLE source_configs;
ALTER TABLE source_configs_old RENAME TO source_configs;

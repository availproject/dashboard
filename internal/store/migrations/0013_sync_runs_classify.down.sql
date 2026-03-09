CREATE TABLE sync_runs_old (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id     INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    scope       TEXT    NOT NULL CHECK(scope IN ('team', 'org', 'notion_workspace', 'github_repo', 'metrics_url')),
    status      TEXT    NOT NULL CHECK(status IN ('running', 'done', 'error', 'failed', 'completed')),
    error       TEXT,
    started_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME
);

INSERT INTO sync_runs_old SELECT * FROM sync_runs WHERE scope != 'classify';
DROP TABLE sync_runs;
ALTER TABLE sync_runs_old RENAME TO sync_runs;

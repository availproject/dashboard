CREATE TABLE IF NOT EXISTS sync_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id     INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    scope       TEXT    NOT NULL CHECK(scope IN ('team', 'org')),
    status      TEXT    NOT NULL CHECK(status IN ('running', 'done', 'error')),
    error       TEXT,
    started_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME
);

CREATE TABLE IF NOT EXISTS sprint_meta (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id       INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    plan_type     TEXT    NOT NULL CHECK(plan_type IN ('current', 'next')),
    sprint_number INTEGER,
    start_date    TEXT,
    end_date      TEXT,
    raw_content   TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(team_id, plan_type)
);

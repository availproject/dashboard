-- SQLite does not support DROP COLUMN; recreate the table without notion_user_id.
CREATE TABLE team_members_old (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id      INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    github_login TEXT,
    role         TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO team_members_old (id, team_id, name, github_login, role, created_at)
    SELECT id, team_id, name, github_login, role, created_at FROM team_members;
DROP TABLE team_members;
ALTER TABLE team_members_old RENAME TO team_members;

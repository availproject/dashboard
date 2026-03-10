-- SQLite does not support DROP COLUMN prior to 3.35.0; recreate the table.
CREATE TABLE teams_old (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO teams_old(id,name,created_at) SELECT id,name,created_at FROM teams;
DROP TABLE teams;
ALTER TABLE teams_old RENAME TO teams;

CREATE TABLE marketing_page_cache (
    page_id     TEXT PRIMARY KEY,
    last_edited TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT '',
    assignee    TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    fetched_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

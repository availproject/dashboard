CREATE TABLE source_catalogue_old (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type   TEXT    NOT NULL CHECK(source_type IN ('github_repo', 'notion_page', 'notion_db', 'grafana_panel', 'posthog_panel', 'signoz_panel')),
    external_id   TEXT    NOT NULL,
    title         TEXT    NOT NULL,
    url           TEXT,
    source_meta   TEXT,
    ai_suggestion TEXT,
    status        TEXT    NOT NULL DEFAULT 'untagged' CHECK(status IN ('untagged', 'configured', 'ignored')),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_type, external_id)
);

INSERT INTO source_catalogue_old
    SELECT * FROM source_catalogue
    WHERE source_type IN ('github_repo', 'notion_page', 'notion_db', 'grafana_panel', 'posthog_panel', 'signoz_panel');
DROP TABLE source_catalogue;
ALTER TABLE source_catalogue_old RENAME TO source_catalogue;

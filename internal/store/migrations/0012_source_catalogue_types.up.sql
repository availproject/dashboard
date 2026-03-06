-- Widen source_catalogue.source_type to include all types produced by connectors.
-- github_label, github_project, github_md_file were missing; posthog_panel was
-- wrong (connector produces posthog_insight). SQLite requires table recreation
-- to change a CHECK constraint.
CREATE TABLE source_catalogue_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type   TEXT    NOT NULL CHECK(source_type IN (
                      'notion_page', 'notion_db',
                      'github_label', 'github_project', 'github_md_file',
                      'grafana_panel', 'posthog_insight', 'signoz_panel'
                  )),
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

INSERT INTO source_catalogue_new SELECT * FROM source_catalogue;
DROP TABLE source_catalogue;
ALTER TABLE source_catalogue_new RENAME TO source_catalogue;

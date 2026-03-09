-- Add parent_id to source_catalogue for hierarchical display.
-- Also adds 'github_repo' as a valid source_type (repos are now emitted as
-- root catalogue items during discovery).
CREATE TABLE source_catalogue_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type   TEXT    NOT NULL CHECK(source_type IN (
                      'notion_page', 'notion_db',
                      'github_label', 'github_project', 'github_repo', 'github_md_file',
                      'grafana_panel', 'posthog_insight', 'signoz_panel'
                  )),
    external_id   TEXT    NOT NULL,
    title         TEXT    NOT NULL,
    url           TEXT,
    source_meta   TEXT,
    parent_id     INTEGER,
    ai_suggestion TEXT,
    status        TEXT    NOT NULL DEFAULT 'untagged' CHECK(status IN ('untagged', 'configured', 'ignored')),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_type, external_id)
);

INSERT INTO source_catalogue_new
    (id, source_type, external_id, title, url, source_meta, parent_id, ai_suggestion, status, created_at, updated_at)
SELECT
    id, source_type, external_id, title, url, source_meta, NULL, ai_suggestion, status, created_at, updated_at
FROM source_catalogue;

DROP TABLE source_catalogue;
ALTER TABLE source_catalogue_new RENAME TO source_catalogue;

-- SQLite does not support DROP COLUMN on older versions; rebuild the table.
CREATE TABLE source_catalogue_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type   TEXT NOT NULL,
    external_id   TEXT NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    url           TEXT,
    source_meta   TEXT,
    parent_id     INTEGER REFERENCES source_catalogue(id) ON DELETE SET NULL,
    ai_suggestion TEXT,
    status        TEXT NOT NULL DEFAULT 'untagged' CHECK(status IN ('untagged','configured','ignored')),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_type, external_id)
);
INSERT INTO source_catalogue_new SELECT id,source_type,external_id,title,url,source_meta,parent_id,ai_suggestion,status,created_at,updated_at FROM source_catalogue;
DROP TABLE source_catalogue;
ALTER TABLE source_catalogue_new RENAME TO source_catalogue;

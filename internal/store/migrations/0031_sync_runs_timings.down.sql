-- SQLite does not support DROP COLUMN in older versions; recreate without timings.
CREATE TABLE sync_runs_new AS SELECT id, team_id, scope, status, error, started_at, finished_at FROM sync_runs;
DROP TABLE sync_runs;
ALTER TABLE sync_runs_new RENAME TO sync_runs;

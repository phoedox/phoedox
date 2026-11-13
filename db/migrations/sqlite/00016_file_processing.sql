-- +goose Up
-- Background jobs (claimed with SKIP LOCKED on Postgres), file versions,
-- previews and extracted text, storage quotas and resumable (tus) uploads.
CREATE TABLE jobs (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  kind         TEXT NOT NULL,
  payload      TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
  attempts     INTEGER NOT NULL DEFAULT 0,
  run_at       TEXT NOT NULL,
  locked_until TEXT,
  last_error   TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX jobs_due ON jobs (status, run_at);

CREATE TABLE file_versions (
  file_id    TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  version    INTEGER NOT NULL,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  sha256     TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (file_id, version)
);
CREATE INDEX file_versions_sha ON file_versions (sha256);
INSERT INTO file_versions (file_id, version, tenant_id, sha256, mime, size, created_by, created_at)
  SELECT id, 1, tenant_id, sha256, mime, size, created_by, created_at FROM files;

ALTER TABLE files ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE files ADD COLUMN thumb_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE files ADD COLUMN text_status TEXT NOT NULL DEFAULT '';
CREATE INDEX files_thumb ON files (thumb_sha256);

ALTER TABLE projects ADD COLUMN quota_mb INTEGER NOT NULL DEFAULT 0;

CREATE TABLE uploads (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  length     INTEGER NOT NULL,
  received   INTEGER NOT NULL DEFAULT 0,
  meta       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE uploads;
ALTER TABLE projects DROP COLUMN quota_mb;
DROP INDEX files_thumb;
ALTER TABLE files DROP COLUMN text_status;
ALTER TABLE files DROP COLUMN thumb_sha256;
ALTER TABLE files DROP COLUMN version;
DROP TABLE file_versions;
DROP TABLE jobs;

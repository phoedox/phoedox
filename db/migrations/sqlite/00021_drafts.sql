-- +goose Up
-- Drafts (ADR-0027): live editing works on a draft per page; the published
-- content changes only on publish. Live logs from before edited the
-- published page directly; they are dropped so drafts start clean.
DELETE FROM doc_updates;
CREATE TABLE drafts (
  document_id  TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  base_version INTEGER NOT NULL,
  title        TEXT NOT NULL,
  category     TEXT NOT NULL DEFAULT '',
  markdown     TEXT NOT NULL,
  participants TEXT NOT NULL DEFAULT '',
  started_by   TEXT NOT NULL,
  started_at   TEXT NOT NULL,
  updated_by   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  reminded_at  TEXT
);
CREATE INDEX drafts_updated ON drafts (updated_at);

-- +goose Down
DROP TABLE drafts;

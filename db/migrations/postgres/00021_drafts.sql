-- +goose Up
-- Drafts (ADR-0027): live editing works on a draft per page; the published
-- content changes only on publish. Live logs from before edited the
-- published page directly; they are dropped so drafts start clean.
SELECT set_config('phoedox.tenant', '*', true);
DELETE FROM doc_updates;
CREATE TABLE drafts (
  document_id  TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  base_version INTEGER NOT NULL,
  title        TEXT NOT NULL,
  category     TEXT NOT NULL DEFAULT '',
  markdown     TEXT NOT NULL,
  participants TEXT NOT NULL DEFAULT '', -- comma-separated actors
  started_by   TEXT NOT NULL,
  started_at   TIMESTAMPTZ NOT NULL,
  updated_by   TEXT NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL,
  reminded_at  TIMESTAMPTZ
);
CREATE INDEX drafts_updated ON drafts (updated_at);
ALTER TABLE drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON drafts USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE drafts;

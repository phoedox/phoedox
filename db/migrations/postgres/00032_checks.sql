-- +goose Up
-- Monitoring: a request block of a page that runs on a schedule. Its state
-- changes write events and mails, and may set the state of an entry.
CREATE TABLE checks (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  document_id  TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  block_index  INTEGER NOT NULL,
  entity_id    TEXT REFERENCES entities(id) ON DELETE SET NULL,
  name         TEXT NOT NULL DEFAULT '',
  interval_sec INTEGER NOT NULL,
  enabled      BOOLEAN NOT NULL DEFAULT TRUE,
  last_status  TEXT NOT NULL DEFAULT 'unknown',
  last_run     TIMESTAMPTZ,
  last_change  TIMESTAMPTZ,
  last_ms      INTEGER NOT NULL DEFAULT 0,
  last_message TEXT NOT NULL DEFAULT '', -- i18n key
  last_detail  TEXT NOT NULL DEFAULT '',
  fail_count   INTEGER NOT NULL DEFAULT 0,
  created_by   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX checks_project ON checks(project_id);
ALTER TABLE checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE checks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON checks USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE checks;

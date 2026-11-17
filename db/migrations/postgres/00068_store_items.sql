-- +goose Up
-- Themes and app templates from the store: a theme remembers the store item
-- and version it came from (update notices, rollback).
ALTER TABLE themes ADD COLUMN origin_id TEXT NOT NULL DEFAULT '';
ALTER TABLE themes ADD COLUMN origin_version TEXT NOT NULL DEFAULT '';

-- Requests for store items from people the organization's store access
-- rules do not let install them; organization admins answer.
CREATE TABLE store_requests (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  org_id       TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  item_type    TEXT NOT NULL,
  item_id      TEXT NOT NULL,
  item_name    TEXT NOT NULL,
  version      TEXT NOT NULL,
  title        TEXT NOT NULL DEFAULT '',
  note         TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'pending',
  answer       TEXT NOT NULL DEFAULT '',
  result       TEXT NOT NULL DEFAULT '',
  requested_by TEXT NOT NULL,
  decided_by   TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL,
  decided_at   TIMESTAMPTZ
);
CREATE INDEX store_requests_org ON store_requests(org_id, status);
ALTER TABLE store_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE store_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON store_requests USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE store_requests;
ALTER TABLE themes DROP COLUMN origin_version;
ALTER TABLE themes DROP COLUMN origin_id;

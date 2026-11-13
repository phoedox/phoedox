-- +goose Up
-- Same columns as on Postgres; SQLite has no row-level security and runs
-- only single-tenant.
CREATE TABLE tenant_domains (
  domain     TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE project_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE sessions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE api_tokens ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE document_versions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE document_links ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE group_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE project_groups ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';

-- +goose Down
ALTER TABLE project_members DROP COLUMN tenant_id;
ALTER TABLE sessions DROP COLUMN tenant_id;
ALTER TABLE api_tokens DROP COLUMN tenant_id;
ALTER TABLE document_versions DROP COLUMN tenant_id;
ALTER TABLE document_links DROP COLUMN tenant_id;
ALTER TABLE group_members DROP COLUMN tenant_id;
ALTER TABLE project_groups DROP COLUMN tenant_id;
DROP TABLE tenant_domains;

-- +goose NO TRANSACTION
-- +goose Up
-- Slugs of deleted entities become free again (needed for the trash).
-- SQLite cannot drop a table constraint, so the table is rebuilt (the
-- documented 12-step procedure). Foreign keys are off meanwhile so edges
-- and document links are not cascaded away; the store uses one connection.
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE entities_new (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  name       TEXT NOT NULL,
  attrs      TEXT NOT NULL DEFAULT '{}',
  source     TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  deleted_at TEXT
);
INSERT INTO entities_new (id, tenant_id, project_id, kind, slug, name, attrs, source, created_at, updated_at, deleted_at)
  SELECT id, tenant_id, project_id, kind, slug, name, attrs, source, created_at, updated_at, deleted_at FROM entities;
DROP TABLE entities;
ALTER TABLE entities_new RENAME TO entities;
CREATE INDEX entities_kind ON entities (project_id, kind) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX entities_slug ON entities (project_id, slug) WHERE deleted_at IS NULL;
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
-- Not reversible without losing deleted entities that share a slug.
SELECT 1;

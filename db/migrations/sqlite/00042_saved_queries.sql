-- +goose Up
-- Saved queries over the knowledge graph (table view, query blocks, MCP).
CREATE TABLE saved_queries (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug       TEXT NOT NULL,
  name       TEXT NOT NULL,
  query      TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX saved_queries_slug ON saved_queries(project_id, slug);

-- +goose Down
DROP TABLE saved_queries;

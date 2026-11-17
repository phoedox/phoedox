-- +goose Up
-- Public apps (apps-definition "Sharing"): an app an owner released to
-- people without an account, at level 0 or with a snapshot of its sources
-- taken with the publisher's rights (by hand or on a schedule). Nothing is
-- read live or written; AI calls only within a monthly limit.
CREATE TABLE app_shares (
  document_id  TEXT NOT NULL PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  token        TEXT NOT NULL UNIQUE,
  mode         TEXT NOT NULL,
  schedule     TEXT NOT NULL DEFAULT 'manual',
  ai_limit     INTEGER NOT NULL DEFAULT 0,
  ai_month     TEXT NOT NULL DEFAULT '',
  ai_used      INTEGER NOT NULL DEFAULT 0,
  snapshot     TEXT NOT NULL DEFAULT '{}',
  snapshot_at  TEXT NOT NULL,
  snapshot_error TEXT NOT NULL DEFAULT '',
  published_by TEXT NOT NULL,
  published_at TEXT NOT NULL
);
CREATE INDEX app_shares_project ON app_shares(project_id);

-- +goose Down
DROP TABLE app_shares;

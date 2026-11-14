-- +goose Up
-- Personal data export and account deletion (ADR-0055).
-- deleted_at marks an account that was deleted: the row stays, anonymised,
-- so shared content keeps its author ("deleted person").
ALTER TABLE users ADD COLUMN deleted_at TEXT;

-- Exports of a person's data: a ZIP in the blob store, downloadable by that
-- person for 7 days, then deleted.
CREATE TABLE user_exports (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status     TEXT NOT NULL DEFAULT 'queued', -- queued | done | failed
  sha256     TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL DEFAULT 0,
  error      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
CREATE INDEX user_exports_user ON user_exports(user_id, created_at);

-- +goose Down
DROP TABLE user_exports;
ALTER TABLE users DROP COLUMN deleted_at;

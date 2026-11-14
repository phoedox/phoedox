-- +goose Up
-- Personal data export and account deletion (ADR-0055).
-- deleted_at marks an account that was deleted: the row stays, anonymised,
-- so shared content keeps its author ("deleted person").
ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ;

-- Exports of a person's data: a ZIP in the blob store, downloadable by that
-- person for 7 days, then deleted.
CREATE TABLE user_exports (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status     TEXT NOT NULL DEFAULT 'queued', -- queued | done | failed
  sha256     TEXT NOT NULL DEFAULT '',
  size       BIGINT NOT NULL DEFAULT 0,
  error      TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX user_exports_user ON user_exports(user_id, created_at);
ALTER TABLE user_exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_exports FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON user_exports USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE user_exports;
ALTER TABLE users DROP COLUMN deleted_at;

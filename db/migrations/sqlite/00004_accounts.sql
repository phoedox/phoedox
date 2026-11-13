-- +goose Up
-- Account lifecycle: disabling, last login, preferred language and one-time
-- links for invitations and password resets.
ALTER TABLE users ADD COLUMN disabled_at TEXT;
ALTER TABLE users ADD COLUMN last_login_at TEXT;
ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT '';

CREATE TABLE account_links (
  id         TEXT PRIMARY KEY,                    -- sha256 of the link token
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose    TEXT NOT NULL CHECK (purpose IN ('invite', 'reset')),
  created_by TEXT NOT NULL,                       -- actor string
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);
CREATE INDEX account_links_user ON account_links (user_id);

-- +goose Down
DROP TABLE account_links;
ALTER TABLE users DROP COLUMN locale;
ALTER TABLE users DROP COLUMN last_login_at;
ALTER TABLE users DROP COLUMN disabled_at;

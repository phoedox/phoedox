-- +goose Up
-- External sign-in identities (OIDC). The subject is unique per issuer.
CREATE TABLE user_identities (
  provider      TEXT NOT NULL,                 -- issuer URL
  subject       TEXT NOT NULL,
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email         TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  last_login_at TEXT,
  PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user ON user_identities (user_id);

-- +goose Down
DROP TABLE user_identities;

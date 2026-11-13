-- +goose Up
-- OAuth 2.1 authorization server for MCP clients (dynamic client
-- registration, authorization code + PKCE, rotating refresh tokens).
CREATE TABLE oauth_clients (
  id            TEXT PRIMARY KEY,               -- client_id
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  name          TEXT NOT NULL,
  client_uri    TEXT NOT NULL DEFAULT '',
  redirect_uris TEXT NOT NULL,                  -- JSON array
  secret_hash   TEXT,                           -- NULL for public clients
  created_at    TEXT NOT NULL,
  last_used_at  TEXT
);

-- One grant per user and client; revoking it ends all of its tokens.
CREATE TABLE oauth_grants (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  client_id    TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scope        TEXT NOT NULL,
  resource     TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at   TEXT
);
CREATE INDEX oauth_grants_user ON oauth_grants (user_id);

CREATE TABLE oauth_codes (
  id             TEXT PRIMARY KEY,              -- sha256 of the code
  tenant_id      TEXT NOT NULL REFERENCES tenants(id),
  grant_id       TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
  redirect_uri   TEXT NOT NULL,
  code_challenge TEXT NOT NULL,
  expires_at     TEXT NOT NULL,
  used_at        TEXT
);

CREATE TABLE oauth_tokens (
  id         TEXT PRIMARY KEY,                  -- sha256 of the token
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  grant_id   TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT                                -- refresh tokens: rotated
);
CREATE INDEX oauth_tokens_grant ON oauth_tokens (grant_id);

-- +goose Down
DROP TABLE oauth_tokens;
DROP TABLE oauth_codes;
DROP TABLE oauth_grants;
DROP TABLE oauth_clients;

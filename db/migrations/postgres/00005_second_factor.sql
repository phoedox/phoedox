-- +goose Up
-- Second factors: TOTP (seed encrypted with the master key), one-time
-- recovery codes and WebAuthn passkeys. auth_challenges holds short-lived
-- login and WebAuthn ceremony state.
CREATE TABLE user_totp (
  user_id      TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  secret_enc   TEXT NOT NULL,
  confirmed_at TIMESTAMPTZ,
  last_step    BIGINT NOT NULL DEFAULT 0,
  created_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE recovery_codes (
  code_hash  TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL,
  used_at    TIMESTAMPTZ
);
CREATE INDEX recovery_codes_user ON recovery_codes (user_id);

CREATE TABLE webauthn_credentials (
  id           TEXT PRIMARY KEY,               -- credential id, base64url
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  credential   TEXT NOT NULL,                  -- JSON of the library's credential
  created_at   TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ
);
CREATE INDEX webauthn_credentials_user ON webauthn_credentials (user_id);

CREATE TABLE auth_challenges (
  id         TEXT PRIMARY KEY,                 -- sha256 of the challenge token
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  data       TEXT NOT NULL DEFAULT '',
  attempts   INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE auth_challenges;
DROP TABLE webauthn_credentials;
DROP TABLE recovery_codes;
DROP TABLE user_totp;

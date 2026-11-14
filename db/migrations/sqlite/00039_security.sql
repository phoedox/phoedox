-- +goose Up
-- Tamper-evident audit log: every new event carries its position in the
-- tenant's chain and a hash over the previous hash and its own content.
ALTER TABLE events ADD COLUMN seq INTEGER;
ALTER TABLE events ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX events_chain ON events (tenant_id, seq) WHERE seq IS NOT NULL;

-- One-time secret links: only ciphertext; the key stays in the link's #fragment.
CREATE TABLE secret_links (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  ciphertext  TEXT NOT NULL,
  passphrase  INTEGER NOT NULL DEFAULT 0,
  expires_at  TEXT NOT NULL,
  created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL
);
CREATE INDEX secret_links_expiry ON secret_links (expires_at);


-- Environment secrets kept in an external vault: a reference instead of a value.
ALTER TABLE project_env ADD COLUMN vault_ref TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE project_env DROP COLUMN vault_ref;
DROP TABLE secret_links;
DROP INDEX events_chain;
ALTER TABLE events DROP COLUMN hash;
ALTER TABLE events DROP COLUMN prev_hash;
ALTER TABLE events DROP COLUMN seq;

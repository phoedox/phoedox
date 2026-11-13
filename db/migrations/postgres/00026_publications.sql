-- +goose Up
-- Publications (ADR-0006, T5e): one or more projects as a public or
-- password-protected site, on a path of the instance or an own domain.
CREATE TABLE publications (
  id            TEXT PRIMARY KEY,
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  slug          TEXT NOT NULL,
  title         TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  projects      TEXT NOT NULL, -- comma-separated project ids, in menu order
  access        TEXT NOT NULL DEFAULT 'public', -- public | password
  password_hash TEXT NOT NULL DEFAULT '',
  domain        TEXT NOT NULL DEFAULT '', -- own host, e.g. docs.example.org
  color         TEXT NOT NULL DEFAULT '',
  css           TEXT NOT NULL DEFAULT '',
  noindex       BOOLEAN NOT NULL DEFAULT FALSE,
  created_by    TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL,
  updated_at    TIMESTAMPTZ NOT NULL,
  UNIQUE (tenant_id, slug)
);
CREATE UNIQUE INDEX publications_domain ON publications (domain) WHERE domain <> '';
ALTER TABLE publications ENABLE ROW LEVEL SECURITY;
ALTER TABLE publications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON publications USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE publications;

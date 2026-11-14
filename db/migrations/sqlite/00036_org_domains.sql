-- +goose Up
-- An organization's own address for the app (e.g. docs.example.com). It is
-- used only after a DNS TXT record proved control over the domain.
CREATE TABLE org_domains (
  org_id      TEXT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  domain      TEXT NOT NULL UNIQUE,
  token       TEXT NOT NULL,
  verified_at TEXT,
  created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL
);

-- +goose Down
DROP TABLE org_domains;

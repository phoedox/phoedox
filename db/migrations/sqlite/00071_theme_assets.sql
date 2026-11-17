-- +goose Up
-- Files of themes (woff2 fonts, logos): stored by content hash and served
-- publicly, because themes reach sign-in pages, publications and e-mails.
CREATE TABLE theme_assets (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  sha256     TEXT NOT NULL,
  kind       TEXT NOT NULL, -- font | logo
  mime       TEXT NOT NULL,
  name       TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, sha256)
);

-- +goose Down
DROP TABLE theme_assets;

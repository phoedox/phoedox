-- +goose Up
-- Plugins: installed per tenant by the (tenant) admin, enabled per project by
-- its owner. The package itself is a blob (content-addressed by sha256).
CREATE TABLE plugins (
  tenant_id    TEXT NOT NULL REFERENCES tenants(id),
  id           TEXT NOT NULL,
  version      TEXT NOT NULL,
  name         TEXT NOT NULL,
  manifest     TEXT NOT NULL,
  sha256       TEXT NOT NULL,
  size         INTEGER NOT NULL,
  source       TEXT NOT NULL CHECK (source IN ('store', 'upload')),
  signed_by    TEXT NOT NULL DEFAULT '', -- key id of the signature; '' = unsigned (development)
  enabled      INTEGER NOT NULL DEFAULT 1,
  recall       TEXT NOT NULL DEFAULT '' CHECK (recall IN ('', 'warning', 'critical')),
  recall_note  TEXT NOT NULL DEFAULT '',
  latest       TEXT NOT NULL DEFAULT '', -- newest compatible version in the store
  license      TEXT NOT NULL DEFAULT '', -- signed license file (JSON)
  installed_by TEXT NOT NULL,
  installed_at TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (tenant_id, id)
);

CREATE TABLE project_plugins (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  plugin_id  TEXT NOT NULL,
  enabled_by TEXT NOT NULL, -- the plugin acts for this user in scheduled runs
  enabled_at TEXT NOT NULL,
  secrets    TEXT NOT NULL DEFAULT '', -- sealed JSON object
  last_run   TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, plugin_id)
);

CREATE TABLE plugin_kv (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  plugin_id  TEXT NOT NULL,
  key        TEXT NOT NULL,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, plugin_id, key)
);

-- +goose Down
DROP TABLE plugin_kv;
DROP TABLE project_plugins;
DROP TABLE plugins;

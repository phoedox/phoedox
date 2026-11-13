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
  size         BIGINT NOT NULL,
  source       TEXT NOT NULL CHECK (source IN ('store', 'upload')),
  signed_by    TEXT NOT NULL DEFAULT '', -- key id of the signature; '' = unsigned (development)
  enabled      BOOLEAN NOT NULL DEFAULT true,
  recall       TEXT NOT NULL DEFAULT '' CHECK (recall IN ('', 'warning', 'critical')),
  recall_note  TEXT NOT NULL DEFAULT '',
  latest       TEXT NOT NULL DEFAULT '', -- newest compatible version in the store
  license      TEXT NOT NULL DEFAULT '', -- signed license file (JSON)
  installed_by TEXT NOT NULL,
  installed_at TIMESTAMPTZ NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, id)
);

CREATE TABLE project_plugins (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  plugin_id  TEXT NOT NULL,
  enabled_by TEXT NOT NULL, -- the plugin acts for this user in scheduled runs
  enabled_at TIMESTAMPTZ NOT NULL,
  secrets    TEXT NOT NULL DEFAULT '', -- sealed JSON object
  last_run   TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, plugin_id)
);

CREATE TABLE plugin_kv (
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  plugin_id  TEXT NOT NULL,
  key        TEXT NOT NULL,
  value      TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (project_id, plugin_id, key)
);
ALTER TABLE plugins ENABLE ROW LEVEL SECURITY;
ALTER TABLE plugins FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON plugins USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE project_plugins ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_plugins FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_plugins USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE plugin_kv ENABLE ROW LEVEL SECURITY;
ALTER TABLE plugin_kv FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON plugin_kv USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE plugin_kv;
DROP TABLE project_plugins;
DROP TABLE plugins;

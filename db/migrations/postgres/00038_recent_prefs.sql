-- +goose Up
-- Recently opened pages and entries per person (the last 200), and personal
-- preferences such as keyboard shortcuts.
CREATE TABLE recent_views (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  ref_id     TEXT NOT NULL,
  viewed_at  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (user_id, ref_id)
);
CREATE INDEX recent_views_user ON recent_views(user_id, viewed_at);

CREATE TABLE user_prefs (
  user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  shortcuts  TEXT NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL
);
ALTER TABLE recent_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE recent_views FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON recent_views USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');
ALTER TABLE user_prefs ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_prefs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON user_prefs USING (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*') WITH CHECK (tenant_id = current_setting('phoedox.tenant', true) OR current_setting('phoedox.tenant', true) = '*');

-- +goose Down
DROP TABLE user_prefs;
DROP TABLE recent_views;

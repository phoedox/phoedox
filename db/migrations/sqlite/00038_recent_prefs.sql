-- +goose Up
-- Recently opened pages and entries per person (the last 200), and personal
-- preferences such as keyboard shortcuts.
CREATE TABLE recent_views (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  ref_id     TEXT NOT NULL,
  viewed_at  TEXT NOT NULL,
  PRIMARY KEY (user_id, ref_id)
);
CREATE INDEX recent_views_user ON recent_views(user_id, viewed_at);

CREATE TABLE user_prefs (
  user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  shortcuts  TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);


-- +goose Down
DROP TABLE user_prefs;
DROP TABLE recent_views;

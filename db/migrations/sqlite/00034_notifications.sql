-- +goose Up
-- Notifications for one person (mentions, replies, failing checks, tasks),
-- shown in the app and sent by mail (at once or as a daily digest), ntfy
-- or web push.
CREATE TABLE notifications (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  subject_id TEXT NOT NULL DEFAULT '',
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  link       TEXT NOT NULL DEFAULT '',
  actor_name TEXT NOT NULL DEFAULT '',
  read_at    TEXT,
  mailed_at  TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX notifications_user ON notifications(user_id, created_at);

CREATE TABLE notification_settings (
  user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  email       TEXT NOT NULL DEFAULT 'instant',
  ntfy_url    TEXT NOT NULL DEFAULT '',
  muted       TEXT NOT NULL DEFAULT '',
  last_digest TEXT,
  updated_at  TEXT NOT NULL
);

CREATE TABLE push_subscriptions (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE push_subscriptions;
DROP TABLE notification_settings;
DROP TABLE notifications;

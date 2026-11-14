-- +goose Up
-- Incidents: opened when a check fails (or by hand), with a timeline of
-- updates. Checks and incidents marked public appear on the project's
-- public status page and on publications.
CREATE TABLE incidents (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  check_id    TEXT REFERENCES checks(id) ON DELETE SET NULL,
  title       TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'investigating',
  public      INTEGER NOT NULL DEFAULT 0,
  started_at  TEXT NOT NULL,
  resolved_at TEXT,
  created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX incidents_project ON incidents(project_id, started_at);

CREATE TABLE incident_updates (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  incident_id TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  status      TEXT NOT NULL,
  body        TEXT NOT NULL DEFAULT '',
  auto        INTEGER NOT NULL DEFAULT 0,
  created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL
);
CREATE INDEX incident_updates_incident ON incident_updates(incident_id, created_at);


ALTER TABLE checks ADD COLUMN public INTEGER NOT NULL DEFAULT 0;
-- Random part of the public status page address (NULL = no public page).
ALTER TABLE projects ADD COLUMN status_token TEXT;
CREATE UNIQUE INDEX projects_status_token ON projects(status_token);
-- Matrix as a notification channel; the access token is sealed with the master key.
ALTER TABLE notification_settings ADD COLUMN matrix_homeserver TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_settings ADD COLUMN matrix_room TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_settings ADD COLUMN matrix_token TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE notification_settings DROP COLUMN matrix_token;
ALTER TABLE notification_settings DROP COLUMN matrix_room;
ALTER TABLE notification_settings DROP COLUMN matrix_homeserver;
DROP INDEX projects_status_token;
ALTER TABLE projects DROP COLUMN status_token;
ALTER TABLE checks DROP COLUMN public;
DROP TABLE incident_updates;
DROP TABLE incidents;

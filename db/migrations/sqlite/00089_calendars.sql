-- +goose Up
-- Calendars (ADR-0089): external calendars of a project (CalDAV or an iCal
-- URL, the password sealed), their events as last read, the Phoedox dates
-- written into them when write_back is on, and the secret iCal feed of a
-- project's dates.
CREATE TABLE calendars (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  kind        TEXT NOT NULL,
  url         TEXT NOT NULL,
  username    TEXT NOT NULL DEFAULT '',
  secret      TEXT NOT NULL DEFAULT '',
  color       TEXT NOT NULL DEFAULT '',
  write_back  BOOLEAN NOT NULL DEFAULT 0,
  synced_at   TEXT,
  last_error  TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX calendars_project ON calendars (project_id);

CREATE TABLE calendar_events (
  calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
  uid         TEXT NOT NULL,
  recurrence  TEXT NOT NULL DEFAULT '',
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  summary     TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  location    TEXT NOT NULL DEFAULT '',
  url         TEXT NOT NULL DEFAULT '',
  starts_at   TEXT NOT NULL,
  ends_at     TEXT NOT NULL,
  all_day     BOOLEAN NOT NULL DEFAULT 0,
  PRIMARY KEY (calendar_id, uid, recurrence)
);
CREATE INDEX calendar_events_range ON calendar_events (project_id, starts_at);

CREATE TABLE calendar_pushed (
  calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
  uid         TEXT NOT NULL,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  hash        TEXT NOT NULL,
  etag        TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (calendar_id, uid)
);

CREATE TABLE calendar_feeds (
  token_hash  TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL REFERENCES tenants(id),
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX calendar_feeds_project ON calendar_feeds (project_id);

-- +goose Down
DROP TABLE calendar_feeds;
DROP TABLE calendar_pushed;
DROP TABLE calendar_events;
DROP TABLE calendars;

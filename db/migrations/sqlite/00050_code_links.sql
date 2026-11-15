-- +goose Up
-- Code repositories of a project (ADR-0063): GitHub or Gitea/Forgejo. The
-- token is a secret of the project environment, named by token_var; public
-- repositories need none.
CREATE TABLE code_repos (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  provider   TEXT NOT NULL,
  web_url    TEXT NOT NULL,
  api_url    TEXT NOT NULL,
  owner      TEXT NOT NULL,
  name       TEXT NOT NULL,
  token_var  TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_check TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  UNIQUE (project_id, web_url)
);
CREATE INDEX code_repos_project ON code_repos(project_id);


-- Commits, releases and pull requests linked to a task card. auto: found by
-- Phoedox (the first release that contains a linked commit).
CREATE TABLE task_code_links (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  repo_id    TEXT NOT NULL REFERENCES code_repos(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  ref        TEXT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL DEFAULT '',
  at         TEXT,
  auto       INTEGER NOT NULL DEFAULT 0,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (task_id, repo_id, kind, ref)
);
CREATE INDEX task_code_links_project ON task_code_links(project_id);


-- +goose Down
DROP TABLE task_code_links;
DROP TABLE code_repos;

import { describe, expect, test } from "bun:test";
import { parseDDL, toMermaid, shownTables, withSchema } from "./erd";

describe("dbmodel", () => {
  const sql = `
-- users of the app
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email VARCHAR(200) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL
);
create table if not exists "public"."projects" (
  id text not null,
  owner_id text references users(id) on delete cascade,
  name text,
  amount numeric(10, 2),
  primary key (id)
);
CREATE TABLE members (project_id TEXT, user_id TEXT,
  PRIMARY KEY (project_id, user_id),
  CONSTRAINT fk_u FOREIGN KEY (user_id) REFERENCES users (id),
  FOREIGN KEY (project_id) REFERENCES projects(id));`;

  test("parses tables, keys and references", () => {
    const t = parseDDL(sql);
    expect(t.map((x) => x.name)).toEqual(["users", "projects", "members"]);
    expect(t[0].columns[0]).toMatchObject({ name: "id", pk: true });
    expect(t[0].columns[1]).toMatchObject({ name: "email", type: "VARCHAR(200)", notNull: true });
    expect(t[1].columns.find((c) => c.name === "id")?.pk).toBe(true);
    expect(t[1].columns.find((c) => c.name === "owner_id")?.ref).toEqual({ table: "users", column: "id" });
    expect(t[1].columns.find((c) => c.name === "amount")?.type).toBe("numeric(10,2)");
    expect(t[2].columns.every((c) => c.pk)).toBe(true);
    expect(t[2].columns.find((c) => c.name === "project_id")?.ref?.table).toBe("projects");
  });

  test("renders a Mermaid ER diagram", () => {
    const m = toMermaid(parseDDL(sql));
    expect(m).toContain("erDiagram");
    expect(m).toContain("TEXT id PK");
    expect(m).toContain("TEXT user_id PK,FK");
    expect(m).toContain('users ||--o{ projects : "owner_id"');
    expect(m).toContain('projects ||--o{ members : "project_id"');
  });
});

test("blocks take tables from the project schema", () => {
  const schema = parseDDL("create table users (id int primary key, name text); create table orgs (id int primary key, name text)");
  const own = parseDDL("-- tables: users, ghosts\ncreate table sessions (id int primary key, user_id int references users(id), org_id int references orgs(id))");
  const { tables, refs, missing } = withSchema(own, shownTables("-- tables: users, ghosts\n"), schema);
  expect(tables.map((t) => t.name)).toEqual(["sessions", "users"]);
  expect(refs.map((t) => [t.name, t.columns.map((c) => c.name)])).toEqual([["orgs", ["id"]]]);
  expect(missing).toEqual(["ghosts"]);
});

test("migrations in order give the end state", () => {
  const sql = `-- file: 001.sql
CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT NOT NULL, old TEXT);
CREATE TABLE notes (id TEXT PRIMARY KEY, body TEXT);
-- file: 002.sql
ALTER TABLE users ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE users DROP COLUMN old;
ALTER TABLE notes ADD COLUMN user_id TEXT REFERENCES users(id);
ALTER TABLE notes RENAME COLUMN body TO text;
ALTER TABLE users ADD CONSTRAINT x UNIQUE (email);
DROP TABLE IF EXISTS gone;
CREATE TABLE gone (id int);
DROP TABLE gone;`;
  const t = parseDDL(sql);
  expect(t.map((x) => x.name)).toEqual(["users", "notes"]);
  expect(t[0].columns.map((c) => c.name)).toEqual(["id", "email", "name"]);
  expect(t[1].columns.map((c) => c.name)).toEqual(["id", "text", "user_id"]);
  expect(t[1].columns[2].ref).toEqual({ table: "users", column: "id" });
});

test("repo source line", async () => {
  const { repoSource } = await import("./erd");
  expect(repoSource("from acme/app db/migrations/postgres\n-- tables: users")).toEqual({ repo: "acme/app", path: "db/migrations/postgres" });
  expect(repoSource("CREATE TABLE x (id int);")).toBeUndefined();
});

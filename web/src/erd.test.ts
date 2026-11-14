import { describe, expect, test } from "bun:test";
import { parseDDL, toMermaid } from "./erd";

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

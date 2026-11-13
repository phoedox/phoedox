# Phoedox – App (Server + Web)

Dokumentation mit Wissensnetz: Go-Server mit eingebettetem Lit-Frontend.
Alle Produkt- und Technik-Entscheidungen stehen in `docs/decisions.md` – vor größeren Änderungen lesen
und bei neuen Entscheidungen dort nachtragen. Der klickbare Prototyp liegt in `docs/prototype/`.

## Aufbau

- `cmd/phoedox` – Server (API `/api/v1`, MCP `/mcp`, Realtime `/rt`, Web-App)
- `cmd/phx` – CLI, inkl. Notfall-Befehle (`phx admin …`), die ohne Plugins funktionieren müssen
- `internal/config` – Konfiguration aus `PHOEDOX_*`-Umgebungsvariablen
- `internal/graph` – Einträge, Kanten, Slugs, Erwähnungen (Kern des Wissensnetzes)
- `internal/store` – Persistenz; SQL in `db/queries/{postgres,sqlite}`, generiert mit sqlc
- `internal/{auth,mcp,realtime,plugins,agenthub}` – noch Gerüst, siehe Paket-Doku
- `db/migrations/{postgres,sqlite}` – goose-Migrationen, **beide Dialekte immer gemeinsam ändern**
- `web/` – Lit + Vite + Bun; `web/dist` wird per `go:embed` eingebunden

## Befehle

- `make test` – Go- und Bun-Tests
- `make lint` – gofmt, go vet, TypeScript
- `make build` – Web bauen, dann Binaries nach `bin/`
- Dev: `go run ./cmd/phoedox` und `cd web && bun run dev` (Proxy auf :8080)

## Regeln

- Code, Bezeichner und Kommentare auf Englisch; Oberflächentexte nur über i18n (`web/src/i18n`, Deutsch + Englisch, beide Tabellen pflegen).
- Commits nach Conventional Commits (`feat:`, `fix:`, `docs:` …) – Release-Please erzeugt daraus Versionen.
- Jede Tabelle hat `tenant_id`. Neue Abfragen müssen im Multi-Tenant-Modus unter Row-Level-Security funktionieren.
- Alles, was im Namen eines Nutzers passiert (UI, MCP, Plugin, Agent), schreibt ein Event mit `actor`.
- Keine Geheimnisse in Dokument-Inhalten; Request-Blöcke referenzieren `{{variablen}}` der Projekt-Umgebung.
- Farben nur über Tokens (`web/src/styles/tokens.css`); Projektfarbe überschreibt `--accent` über `--pl`/`--pd`.
- Icons ausschließlich aus Lucide (dann Heroicons, dann Feather); keine eigenen Icons.

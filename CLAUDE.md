# Phoedox – App (Server + Web)

Dokumentation mit Wissensnetz: Go-Server mit eingebettetem Lit-Frontend.
Alle Produkt- und Technik-Entscheidungen stehen in **Phoedox selbst**: https://app.phoedox.com, Projekt
`phoedox`, Kategorie „Entscheidungen“ (ADR-0001 …). Vor größeren Änderungen per MCP lesen (`list_decisions`,
`get_document`), neue Entscheidungen mit `create_decision` anlegen (bei Ersatz `supersedes`). Ohne MCP-Zugang
den Nutzer fragen. `docs/decisions.md` ist nur noch ein Verweis (ADR-0026). Der klickbare Prototyp liegt in `docs/prototype/`.

## Aufbau

- `cmd/phoedox` – Server (API `/api/v1`, MCP `/mcp`, Realtime `/rt`, Web-App)
- `cmd/phx` – CLI, inkl. Notfall-Befehle (`phx admin …`), die ohne Plugins funktionieren müssen
- `internal/config` – Konfiguration aus `PHOEDOX_*`-Umgebungsvariablen
- `internal/graph` – Einträge, Kanten, Slugs, Erwähnungen (Kern des Wissensnetzes)
- `internal/store` – Persistenz für Postgres und SQLite; SQL mit `?`-Platzhaltern (Dialekt-Helfer in `db.go`), Tests laufen gegen beide (`PHOEDOX_TEST_PG`)
- `internal/core` – Geschäftslogik: Rechte, Validierung, Erwähnungen, Vorschläge für KI-Schreibzugriffe
- `internal/auth` – Passwörter (argon2id), Sitzungen, API-Tokens
- `internal/mcp` – MCP-Server (`/mcp`, Bearer-Token), Tools für Projekte, Dokumente, Entscheidungen, Einträge
- `internal/app` – verbindet Konfiguration, DB und Dienste (Server + CLI)
- `resources/prompts/` – System-Prompts und Anleitungen für KI als Markdown, eingebettet beim Build, per `PHOEDOX_PROMPTS_DIR` beim Start überschreibbar. **Prompt-Texte nie im Go-Code**, immer hier ablegen und mit `resources.Prompt(name)` laden.
- `internal/plugins` – WASM-Laufzeit (Extism), Manifest, Pakete, Signaturen; Kernlogik in `internal/core/plugins.go`, Store-Client `internal/storeclient`. Test-Plugin in `internal/plugins/testdata/testplugin` (wird in Tests gebaut)
- `internal/agenthub` – noch Gerüst, siehe Paket-Doku
- `db/migrations/{postgres,sqlite}` – goose-Migrationen, **beide Dialekte immer gemeinsam ändern**
- `web/` – Lit + Vite + Bun; `web/dist` wird per `go:embed` eingebunden

## Befehle

- `make test` – Go- und Bun-Tests
- `make lint` – gofmt, go vet, TypeScript
- `make build` – Web bauen, dann Binaries nach `bin/`
- Dev: `go run ./cmd/phoedox` und `cd web && bun run dev` (Proxy auf :8080)

## Regeln

- Code, Bezeichner und Kommentare auf Englisch; Oberflächentexte nur über i18n (`web/src/i18n`, Deutsch + Englisch, beide Tabellen pflegen).
- **Fehlermeldungen übersetzen:** Validierungsfehler mit Schlüssel (`invalid("validation.xyz", params, …)`), die API liefert `key` + `params`; jeder Schlüssel braucht Einträge in beiden i18n-Tabellen. Server-Texte (`error`) nie direkt im Web anzeigen.
- Immer die aktuelle stabile Go-Version und aktuelle Abhängigkeiten verwenden.
- Commits nach Conventional Commits (`feat:`, `fix:`, `docs:` …) – Release-Please erzeugt daraus Versionen.
- Jede Tabelle hat `tenant_id`. Neue Abfragen müssen im Multi-Tenant-Modus unter Row-Level-Security funktionieren.
- Alles, was im Namen eines Nutzers passiert (UI, MCP, Plugin, Agent), schreibt ein Event mit `actor`.
- Keine Geheimnisse in Dokument-Inhalten; Request-Blöcke referenzieren `{{variablen}}` der Projekt-Umgebung.
- Farben nur über Tokens (`web/src/styles/tokens.css`); Projektfarbe überschreibt `--accent` über `--pl`/`--pd`.
- Icons ausschließlich aus Lucide (dann Heroicons, dann Feather); keine eigenen Icons.

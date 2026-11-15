# Phoedox – App (Server + Web)

Dokumentation mit Wissensnetz: Go-Server mit eingebettetem Lit-Frontend.
Alle Produkt- und Technik-Entscheidungen stehen in **Phoedox selbst**: https://app.phoedox.com, Projekt
`phoedox`, Kategorie „Decisions“ (ADR-0001 …), offene Arbeit auf dem Board „Roadmap“ (Spalten Backlog, To do, In progress, Needs Jan, Done). Vor größeren Änderungen per MCP lesen (`list_decisions`,
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
- `resources/skills/phoedox/` – der Phoedox-Skill für KI-Clients (SKILL.md + references, als `/phoedox-skill.zip` ausgeliefert). **Bei jeder Änderung, die KI-Clients betrifft (MCP-Tools, Parameter, neue Möglichkeiten wie Entscheidungs-Optionen, neue Funktionen, auf die die KI Nutzer hinweisen kann), Skill und `resources/prompts/mcp-instructions.md` im selben Commit anpassen.**
- `resources/prompts/` – System-Prompts und Anleitungen für KI als Markdown, eingebettet beim Build, per `PHOEDOX_PROMPTS_DIR` beim Start überschreibbar. **Prompt-Texte nie im Go-Code**, immer hier ablegen und mit `resources.Prompt(name)` laden.
- `internal/plugins` – WASM-Laufzeit (Extism), Manifest, Pakete, Signaturen; Kernlogik in `internal/core/plugins.go`, Store-Client `internal/storeclient`. Test-Plugin in `internal/plugins/testdata/testplugin` (wird in Tests gebaut)
- `internal/agenthub` – noch Gerüst, siehe Paket-Doku
- `db/migrations/{postgres,sqlite}` – goose-Migrationen, **beide Dialekte immer gemeinsam ändern**
- `web/` – Lit + Vite + Bun; `web/dist` wird per `go:embed` eingebunden
- `website/` – Produkt-Webseite phoedox.com als eigener Dienst (ADR-0054): Vorlage + i18n (de/en), Server `cmd/phoedox-website` (`internal/website`), Image `phoedox-website`, Chart `deploy/helm/phoedox-website`

## Befehle

- `make test` – Go- und Bun-Tests
- `make lint` – gofmt, go vet, TypeScript
- `make build` – Web bauen, dann Binaries nach `bin/`
- Dev: `go run ./cmd/phoedox` und `cd web && bun run dev` (Proxy auf :8080)

## Regeln

- **Dokumentation und Aufgaben auf Englisch:** README, `docs/`, Phoedox-Seiten, Entscheidungen (ADRs) und Karten auf dem Board „Roadmap“ immer auf Englisch schreiben; Deutsch nur als Übersetzung (Projekt `phoedox` hat Englisch als Hauptsprache).
- **Aufgaben aus dem Chat sofort auf die Roadmap:** Was Jan im Chat als Aufgabe oder Wunsch nennt, gleich als Karte auf dem Board „Roadmap“ im Projekt `phoedox` anlegen (vorher `list_tasks` gegen Doppelte), beim Start nach „In progress“ schieben und beim Abschluss nach „Done“.
- Code, Bezeichner und Kommentare auf Englisch; Oberflächentexte nur über i18n (`web/src/i18n`, Deutsch + Englisch, beide Tabellen pflegen). Nach Änderungen an `kind.*`/`rel.*`/`attr.*`/`opt.*` `go run ./internal/publish/labelsgen` ausführen (Server-Seiten wie die Notfallkopie nutzen sie; ein Test prüft es).
- **Fehlermeldungen übersetzen:** Validierungsfehler mit Schlüssel (`invalid("validation.xyz", params, …)`), die API liefert `key` + `params`; jeder Schlüssel braucht Einträge in beiden i18n-Tabellen. Server-Texte (`error`) nie direkt im Web anzeigen.
- Immer die aktuelle stabile Go-Version und aktuelle Abhängigkeiten verwenden.
- Commits nach Conventional Commits (`feat:`, `fix:`, `docs:` …) – Release-Please erzeugt daraus Versionen.
- Jede Tabelle hat `tenant_id`. Neue Abfragen müssen im Multi-Tenant-Modus unter Row-Level-Security funktionieren.
- Alles, was im Namen eines Nutzers passiert (UI, MCP, Plugin, Agent), schreibt ein Event mit `actor`.
- Keine Geheimnisse in Dokument-Inhalten; Request-Blöcke referenzieren `{{variablen}}` der Projekt-Umgebung.
- Farben nur über Tokens (`web/src/styles/tokens.css`); Projektfarbe überschreibt `--accent` über `--pl`/`--pd`.
- Icons ausschließlich aus Lucide (dann Heroicons, dann Feather); keine eigenen Icons.

## Demo-Projekt

Auf app.phoedox.com gibt es das Projekt `demo` („Phoedox Demo“): Familienhaus mit Homelab, zeigt alle Funktionen ohne Plugins.
- **Nach jedem Update der Cloud** (Jan meldet es, oder `/healthz` zeigt einen neuen Commit) neue Funktionen dort per MCP anlegen und auf dem Board „Household and homelab“ in der Spalte **„New in Phoedox“** je eine Karte zum Ausprobieren anlegen (Titel = Funktion, Beschreibung = wo man sie findet). Alte Karten nach dem nächsten Update nach „Done“ schieben.
- Was noch fehlt, steht auf der Roadmap-Karte „Demo project: show features after the next cloud update“.
- Inhalte bleiben fiktiv (keine echten Namen, Adressen, Telefonnummern), Hauptsprache Englisch mit deutscher Übersetzung der Startseite.


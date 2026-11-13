# Phoedox – Entscheidungen

Stand: 13.11.2026. Ergebnis der Planungsrunden; der klickbare Prototyp liegt in `docs/prototype/`.
Änderungen an diesen Entscheidungen bitte hier nachtragen.

## Produkt

- **Kernidee:** Alle Dokumente schreiben in einen typisierten Graphen aus Einträgen (Entitäten) und Kanten. Markdown, Grundriss, 3D und alle anderen Doku-Typen sind Sichten darauf. Die KI fragt den Graphen über MCP.
- **Aufbau:** Instanz → Organisationen (kommerziell / nicht-kommerziell, nur vom System-Admin gesetzt) → Projekte. Persönliche Bereiche gelten als nicht-kommerziell.
- **Rollen:** System-Admin, Org-Admin, Mitglied. Projektrollen Besitzer / Bearbeiter / Leser + Zusatzrecht „Steuern“. Dokumente können strenger sein als das Projekt. Gruppen pro Organisation, auch aus OIDC. Gäste mit Ablaufdatum, geteilte Lese-Links.
- **Projekt:** Name, Kürzel, Beschreibung, Primärfarbe (hell/dunkel automatisch auf ≥ 4,5:1 Kontrast angepasst), Mitglieder, Umgebung (Variablen + Geheimnisse).
- **Einträge:** ULID intern + änderbarer Slug. Erwähnungen `@slug`, projektübergreifend `@projekt/slug`. Löschen nimmt Kanten mit in den Papierkorb. Duplikate (MAC, Seriennummer, Hostname) werden zum Zusammenführen vorgeschlagen. Jeder Eintrag kann eigene Dokumente haben (`documents`).
- **Hierarchische Grenzen:** Papierkorb-Dauer und Speicherkontingente: System setzt Maximum, Organisation und Nutzer/Projekt dürfen nur darunter bleiben.
- **Favoriten:** Dokumente pro Projekt (für alle Mitglieder, gesetzt von Bearbeitern) oder pro Nutzer (privat) markieren. Sie stehen in der Seitenleiste ganz oben in einer eigenen, leicht hervorgehobenen Gruppe „Favoriten“; Projekt-Favoriten setzen Bearbeiter und Besitzer, eigene Favoriten jeder; beide unterscheidbar, Reihenfolge per Ziehen.
- **Hintergrund-Funktionen** (Gerätesuche, KI-Zusammenfassungen …) sind abschaltbar; Organisationen können sie erzwingen.

## Doku-Typen

**Im Kern:** Markdown (mit Request-Blöcken), Externer Link, Tabelle/Datenbank, Grundriss + 3D + Rack, Topologie, Dashboard, OpenAPI, Entscheidungs-Log (ADR).

**Als offizielle Plugins** (`phoedox-plugins`, beweisen gleichzeitig die Plugin-API): Checkliste/Formular, Glossar, Patchliste/Kabelplan, Netzwerk-Konfiguration, Kalender, Kanban, Zeitleiste/Gantt, Freies Diagramm, Prozess (EPK/eEPK, BPMN 2.0, Swimlanes), Architektur (C4), Sequenzdiagramm, Mindmap/Whiteboard, Datenbankmodell, Komponente, Sicherungskasten/Elektroplan, Karte (Geo), Ausführbares Notebook, Runbook.

- **Externer Link:** Dokumente anderer Systeme stehen als Link-Einträge in der Seitenleiste (Icon `external-link`), in Gruppen/Kategorien und als Favorit möglich, öffnen im neuen Tab. Optional Erreichbarkeits-Prüfung und Archiv-Kopie. Link ist ein Eintrag im Netz und kann mit Geräten verknüpft werden (z. B. Hersteller-Doku ↔ Router).
- **Request-Blöcke** laufen über den Server, mit Erwartungen (`status == 200`, `$.state >= -70`). Geheimnisse bleiben in der Projekt-Umgebung.
- **Grundriss:** Millimeter. Importe: Bild/PDF + Kalibrierung, Apple RoomPlan, DXF/DWG, IFC. Editor: Wände zeichnen oder Räume als Polygone. Fotos auf den Plan ziehen; KI erkennt Geräte/Typenschilder.
- **Serverschränke** sind Kern: `rack`, `server`, `patch_panel`, Kante `mounted_in` mit Position und Höhe (HE).
- **Software-Baum:** `software`, `vm`, `lxc`, `stack`, `container` mit `installed_on`, `runs_on`, `managed_by`, `part_of`.
- **Komponente:** React/Vue/Svelte/Lit/HTML+CSS+JS, Build mit esbuild im Server, Anzeige im Sandbox-iframe auf eigener Herkunft, Steuerung aus `phoedox.component.json`, Aktionen, Varianten, Zoom/Pan.
- **Datenbankmodell:** ER-Diagramm, Import aus DDL/Migrationen/live über Agent (nur Schema), Versionen mit Vergleich und Migrationsskript, Abweichungs-Warnung.

## Technik

| Bereich | Entscheidung |
|---|---|
| Repos | `phoedox` (App: Go-Server + Lit-Web), `phoedox-sdk` (Plugin-SDK), `phoedox-plugins` (offizielle Plugins), `phoedox-agent`, `phoedox-store`, `phoedox-app` (Flutter) |
| Lizenzen | App/Server, Store und Flutter-App AGPL-3.0; SDK, Agent und offizielle Plugins MIT. Store bewusst offen: Sicherheit kommt aus geheimen Schlüsseln und Prüfungen, nicht aus verstecktem Code |
| Releases | SemVer + Release-Please, Conventional Commits |
| Go-Version | Immer die aktuelle stabile Go-Version (derzeit 1.27, `toolchain` in go.mod gepinnt); Abhängigkeiten aktuell halten |
| HTTP | `net/http` + chi |
| DB | goose-Migrationen für Postgres und SQLite. **Abweichung vom Plan:** statt sqlc vorerst handgeschriebenes SQL mit gemeinsamer Abfrageschicht (`?`-Platzhalter, pro Dialekt umgesetzt), weil sqlc pro Dialekt eigene Go-Typen erzeugt; später neu bewerten. Cloud: eine DB, `tenant_id` + Row-Level-Security |
| Suche | In der DB: Postgres-Volltext + `pg_trgm`; SQLite FTS5 mit Trigrammen. Kein eigener Dienst |
| Vektoren/KI | pgvector / sqlite-vec. Standard: kein KI-Anbieter. Eingebauter Chat + MCP |
| Frontend | Lit, eigene Komponenten, Vite, Bun, URL-Routing, Deutsch + Englisch, Farbschema folgt System, Befehlspalette ⌘K, WCAG 2.2 AA |
| 3D | three.js (Web), nativ in Flutter; Modelle prozedural, glTF optional |
| Live-Bearbeitung | Yjs für alle Doku-Typen. Text-Editor Lexical hinter austauschbarer Schnittstelle. Server verteilt/speichert Updates und liest mit yrs (WASM in wazero) |
| Speicherformat | Blöcke als JSON, Markdown-Export; Versionen automatisch + benannt |
| Undo | Standard eigene Änderungen; über die UI auch fremde (mit Protokoll) |
| Anmeldung | Passwort + TOTP, Passkeys, OIDC, Magic Link. 5-min-JWT im Speicher + HttpOnly-Session-Cookie. MCP: OAuth 2.1 + API-Tokens. SMTP |
| MCP | Viele spezielle Tools + Plugin-Tools, Ressourcen, Prompts, Benachrichtigungen. KI-Schreiben pro Projekt einstellbar, Standard Vorschlag. Rate-Limits pro Token + Organisation |
| API | REST `/api/v1`, OpenAPI aus dem Code, versionierte Webhooks. Fehler liefern einen stabilen `code`, einen Übersetzungsschlüssel `key` und `params`; die Web-App zeigt nur übersetzte Texte, `error` ist englischer Rückfall (und für MCP) |
| Dateien | Lokal oder S3, inhaltsadressiert, Versionen; Vorschaubilder, PDF-Suche, OCR, Virenscan; tus-Uploads |
| Betrieb | Geplante Backups (S3/lokal/SFTP) + Wiederherstellung in der UI; Prometheus, OpenTelemetry, JSON-Logs, Statusseite; Ein-Klick-Update mit Backup |
| Cloud | Hetzner + Docker Compose, später Kubernetes / eigene Server. EU, AV-Vertrag, Verschlüsselung at rest pro Organisation, externe KI nur mit Zustimmung |
| Flutter-App | Riverpod für Zustand (Streams aus der Sync-Schicht), flutter_rust_bridge für Dart ↔ Rust (yrs), Version gepinnt |
| Tests | Go-Unit/Integration gegen Postgres + SQLite, Bun-Tests, Playwright, MCP-Vertragstests |

## Plugins & Store

- Backend WASM über Extism auf wazero, Frontend ES-Modul mit Lit. Rechte im Manifest, Grenzen pro Aufruf, Plugin sieht nie mehr als der aufrufende Nutzer.
- Store (`store.phoedox.com`, eigenes Repo, eigene DB) signiert jedes Paket; ausgewählte Plugins brauchen zusätzlich die Autorensignatur. Instanz bekommt die neueste kompatible Version (`engines.phoedox`, `hostApi`).
- Prüfung automatisch + Stichproben in einer Moderations-UI im Browser. **Der Store-Server führt hochgeladene Pakete nie aus.**
- Rückruf: Warnung, Instanz-Admin entscheidet; kritische werden deaktiviert, außer „kritischer Pfad“ (nur isoliert). Immer Notfall-Admin (Break-Glass) und `phx admin` auf dem Server.
- Lizenzen: kostenlos / privat kostenlos + kommerziell kostenpflichtig / kostenpflichtig; später Abos. Kurzer Code → signierte, offline prüfbare Lizenzdatei; gebunden an Organisation + Instanz.
- Autorenanteil im System-Admin einstellbar mit Stufen (z. B. 90/10, 95/5). Gratis-Schlüssel pro Monat über UI und REST-API; Kontingent pro Autor (Verfall oder Übertrag bis Höchstmenge, Schonfrist z. B. 7 Tage, unbegrenzt möglich). Zahlungsanbieter hinter Schnittstelle, später.

## Agent

Go-Binary für Linux amd64/arm64, Windows Server, macOS, FreeBSD/OPNsense/TrueNAS. Ausgehend per WebSocket + mTLS. Lesen (System, Pakete, Container, Images, Stacks, Proxmox-Host-Modus), Steuern, Updates, freie Befehle. Absicherung: lokale Freigabeliste **und** Bestätigung in der UI **und** Signatur des Admins. Installation per Einzeiler mit Token, Docker oder Proxmox-Host-Modus. Selbst-Update signiert; in Docker wahlweise Hinweis oder über Docker-Socket mit Helfer-Container. Messwerte in der DB mit Verdichtung, optional Prometheus. CVE (OSV), End-of-Life, Firmware-Hinweise.

## Home Assistant

Verbindung direkt, über den Agenten oder über ein Relay-Add-on. Phoedox auch als HA-Add-on (SQLite). Übernahme beim Verbinden wählbar (Bereiche, Geräte, Live-Werte, Verlauf). Schalten über eigene HA-Verbindung; Besitzer kann pro Gerät freigeben.

## Veröffentlichungen & Domains

Mehrere Projekte → eine öffentliche Seite mit Projekt-Menü und Kategorien pro Projekt. Zugriff öffentlich / Passwort / SSO. Stand live oder freigegebene Versionen. Eigene Domain per CNAME + TXT, Zertifikate über ACME (CertMagic). System-Admin setzt App-Domain, Basis-Domain und CNAME-Ziel; Org-Admins hängen eigene App-Domains an (z. B. `docs-intern.firma.de`). Föderation: Projekt-Spiegel zwischen Instanzen. PDF-Handbücher mit Vorlagen.

## Phasen

1. **MVP:** Projekte, Organisationen, Rechte, Anmeldung; Markdown + Request-Blöcke (Lexical + Yjs); Suche; MCP; Grundriss + Geräte + Pfad-Diagnose; Agent (lesen) + Software-Baum; Home Assistant (lesen + schalten).
2. 3D + Serverschränke, Veröffentlichungen + Domains, Agent steuern/Updates, Vorlagen, CLI, API + Webhooks.
3. Plugin-SDK + Store + Lizenzen, Komponente, Überwachung + Alarme, Kommentare + Aufgaben, Flutter-App.
4. KI-Helfer, Integrationen, Haushalt, E2E-Geheimnisse, Umzug zwischen Instanzen, Kubernetes.

Weitere ausgewählte Features (Runde 4): Verantwortliche pro Eintrag, Review-Termine für Seiten, Pflichtfelder & Doku-Regeln, Standort-Hierarchie mit Karte; Tabellenansicht mit Massenbearbeitung + CSV, gespeicherte Abfragen, Graph-Explorer, wiederverwendbare Blöcke; Wenn-dann-Regeln, Ticket-Anbindung (Zammad, Jira, GitHub), Kontakte & Lieferanten, Einmal-Links für Geheimnisse; zuletzt geöffnet & Verlauf, Fokus-Modus, anpassbare Kurzbefehle.

Feature-Backlog: siehe Konzept-Prototyp (`docs/prototype/app-prototype.html`, Abschnitte „Feature-Backlog“ und „Entscheidungen Runde 3“).

## Runde 5

**Weitere offizielle Plugins (Integrationen & Werkzeuge):** MikroTik/Cisco/Aruba (SNMP), Synology/TrueNAS, Tailscale/WireGuard, Kubernetes, Hetzner/AWS/Azure-Inventar, Cloudflare/DNS, Uptime Kuma/Zabbix/Checkmk, Grafana, MQTT/Zigbee2MQTT, Shelly/Tasmota/ESPHome, Philips Hue/Matter, Kameras (UniFi Protect, Frigate), Paperless-ngx, Microsoft 365/Google Workspace, DSGVO-Verarbeitungsverzeichnis, BSI-Grundschutz/ISO 27001, Etiketten-Druck (Brother, Dymo), Web-Terminal über den Agenten (nur mit Recht + Protokoll), Wake-on-LAN & Speedtest, Excel/CSV-Inventar-Import.

**Weitere Features:** Mandanten-Übersicht für Dienstleister, Einsätze & Zeiterfassung, gebrandete Organisationen, Vorlagen über Kunden hinweg; Offline-Notfallkopie (statisches HTML auf USB/NAS), Wanddisplay/Kiosk, Sprachfragen über HA Assist, Gast-WLAN-Karte; Zeitstrahl pro Gerät, Ersatzteile & Zubehör, Pflichtlektüre mit Lesebestätigung, Netz-Gesundheitswert.

| Bereich | Entscheidung |
|---|---|
| Abfragen | Eigene kleine Abfragesprache + Klick-Editor, übersetzt nach SQL (beide Dialekte). Pfadtiefe Standard 6, vom Admin einstellbar, Zyklen erkannt. Pfade/Simulation: rekursives SQL als Wahrheit, Graph-Cache im Speicher für große Projekte. Projektübergreifend nur über sichtbare Projekte. |
| Echtzeit | y-websocket-kompatibel (Sync + Awareness). Kompaktierung nach Leerlauf (z. B. 30 s) oder alle N Updates (z. B. 500), Versionen als Schnappschüsse. Awareness: Cursor/Auswahl, Anwesende, Blickposition in 3D/Grundriss, pro Nutzer abschaltbar. Größengrenze mit Warnung (z. B. 20 MB), große Grundrisse pro Geschoss. |
| Rechte | Eigene Engine in Go, als SQL-Filter. Projekt → Ordner → Dokument; tiefere Ebenen dürfen einschränken **und** erweitern (einzelnes Dokument für Projektfremde freigeben). Token/Plugin: Schnittmenge aus Nutzer- und eigenen Rechten. Absicherung: Tests für jede Route (Leser/Fremder/Gast), Row-Level-Security, Audit bei Verweigerung. |
| Hintergrund-Jobs | Eigene Job-Tabelle (Postgres mit SKIP LOCKED, SQLite gleich). Worker im selben Binary, abschaltbar (`--role=worker` für eigene Pods). Cron über Leader-Wahl per Advisory Lock. |
| Verteilung | Schnittstelle `Bus` mit Umsetzungen im Speicher (eine Instanz/SQLite), Postgres LISTEN/NOTIFY (Start mit Kubernetes) und später NATS. Über den Bus laufen nur Hinweise mit IDs (NOTIFY-Grenze 8 KB), Daten immer aus der DB – Wechsel ohne Datenmigration. |
| Frontend | `@lit-labs/router` hinter dünner eigener Schicht (Plugins registrieren Routen, austauschbar). Zustand: `@lit-labs/signals` + Context. UI-Bausteine als `@phoedox/ui` im SDK-Repo (Tokens + Lit-Komponenten) für App, veröffentlichte Seiten und Plugins. Lexical-Kern ohne React, Spezialblöcke als DecoratorNodes mit Lit-Elementen. |
| Veröffentlichungen | Server-Rendering in Go + Lit-Inseln für interaktive Blöcke. Plugins liefern `renderPublic()` (ohne JS lesbar) + optional Insel. Sitemap, robots.txt, Open-Graph-Bilder, kanonische URLs pro Version, Suchmaschinen sperrbar. Design: Nutzer Logo/Farben/Schriften/eigenes CSS; Organisationen komplette Themes für alle Veröffentlichungen, wenn der System-Admin das freischaltet. |
| KI | Eigene schlanke Schnittstelle (Chat, Tools, Embeddings), Adapter Anthropic und OpenAI-kompatibel (inkl. Ollama). Zerlegung pro Block (mit Überschriftenpfad) + Kontextpaket pro Eintrag. Prompt-Injection: Inhalte als Daten markiert, externe Inhalte gekennzeichnet, verdächtige Muster an Admin gemeldet; Schalten/Schreiben bleibt durch die früheren Regeln bestätigungspflichtig. Cloud: eigener API-Schlüssel oder Kontingent im Tarif. |
| Agent-Protokoll | Protobuf über WebSocket. mTLS mit eigener CA pro Instanz (kurze Laufzeit, Erneuerung, Widerruf), externe PKI (step-ca, Vault) einstellbar. Offline: Messwerte begrenzt puffern und nachliefern. Intervall 10 s solange jemand zusieht, sonst 60 s. |
| Geheimnisse & Konfiguration | Envelope-Verschlüsselung: Schlüssel pro Organisation, geschützt durch Master-Key (Datei/Umgebung), optional KMS/Vault. Konfiguration: Umgebung + optionale Datei für den Start, laufende Einstellungen in der DB über den Admin-Bereich. |
| Entwicklung | Dev-Container (Go, Bun, Rust, Postgres). Fehlerberichte opt-in (Self-Hosting aus, Cloud an ohne Inhalte). |
| Projektorganisation | GitHub Issues + Projects über alle Repos, später in Phoedox gespiegelt. Produkt-Doku `docs.phoedox.com` als Phoedox-Veröffentlichung. Beiträge mit CLA (CLA Assistant, Text rechtlich prüfen lassen; deutsches Recht: Nutzungsrechte), CONTRIBUTING, Code of Conduct, SECURITY.md mit privaten Meldungen, Issue-/PR-Vorlagen. |

## Betrieb (erste Version)

| Bereich | Entscheidung |
|---|---|
| Image | `ghcr.io/phoedox/phoedox`, distroless, nonroot; `:main` bei jedem Push, `:X.Y.Z`/`:X.Y`/`:latest` bei Releases (aus `release-please.yml` aufgerufen, da Tags per `GITHUB_TOKEN` keine Workflows auslösen). Tags ohne Komponentenpräfix (`vX.Y.Z`). |
| Client-IP | `X-Forwarded-For` nur mit `PHOEDOX_BEHIND_PROXY=true` vertrauen – sonst ließe sich das Login-Limit per Header umgehen. |
| Cookies | Standard `Secure`; `PHOEDOX_INSECURE_COOKIES=true` nur für HTTP ohne TLS. |
| Zeiten in JSON | Nicht gesetzte Zeiten (z. B. „Token nie benutzt“) als `null`, nicht als Jahr 1. |

## Entscheidungen (ADRs)

| Bereich | Entscheidung |
|---|---|
| Nummern | Pro Projekt fortlaufend (`ADR-0001`), vergeben vom Server über einen Zähler am Projekt (`adr_seq`), nie wiederverwendet – auch nicht nach dem Löschen. Bestehende Entscheidungen wurden beim Update nach Anlagedatum nummeriert. Überall, wo ein Dokument erwartet wird, gilt auch `ADR-0003`, `adr-3` oder `ADR3`. |
| Ersetzen | `supersedes` (Slug oder Nummer) setzt die ältere Entscheidung im selben Schritt auf „Ersetzt“ und trägt `supersededBy` ein – in einer Transaktion, mit Konfliktprüfung. `supersededBy` pflegt nur der Server. Umbenennen eines Slugs zieht die Verweise in beide Richtungen nach. Bei KI-Vorschlägen passiert das erst beim Annehmen. |
| Übersicht | Eigene Seite pro Projekt mit Statusfilter (`?status=`), MCP-Werkzeug `list_decisions`, REST `GET /projects/{p}/decisions`. |

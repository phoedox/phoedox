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

## Konten (erste Ausbaustufe)

| Bereich | Entscheidung |
|---|---|
| Einladen | Nur System-Admins (Web, API-Token, CLI – nicht per MCP). Konto ohne Passwort + Einmal-Link (7 Tage). Mit SMTP per E-Mail, sonst wird der Link dem Admin zum Weitergeben angezeigt. Ein neuer Link macht ältere ungültig. |
| Passwort vergessen | Einmal-Link (1 Stunde) per E-Mail, gleiche Antwort für bekannte und unbekannte Adressen. Nur mit `PHOEDOX_PUBLIC_URL`/`PHOEDOX_DOMAIN` – Links aus dem `Host`-Header ließen sich auf fremde Domains umbiegen. Ohne E-Mail erstellt ein Admin einen Link (24 Stunden). Neues Passwort beendet alle Sitzungen. |
| Deaktivieren | Sperrt Anmeldung, Sitzungen und Tokens sofort; Daten bleiben. Der letzte aktive Admin kann weder deaktiviert noch herabgestuft werden, niemand sperrt sich selbst aus. |
| Passwörter | argon2id, mindestens 10 Zeichen (Server und Web gleich). Passwortänderung braucht das alte Passwort und beendet andere Sitzungen. |
| E-Mails | Nur Text, Vorlagen in `resources/mail` (de/en, per `PHOEDOX_MAIL_DIR` überschreibbar). Sprache des Kontos, sonst `PHOEDOX_DEFAULT_LOCALE`. |
| Sprache | Pro Konto „wie im Browser“, Deutsch oder Englisch. |

## Zwei-Faktor & Passkeys

| Bereich | Entscheidung |
|---|---|
| Schalter | Aktive TOTP-Einrichtung macht die Passwort-Anmeldung zweistufig (Challenge, 5 Minuten, 5 Versuche). Zweiter Schritt: TOTP-Code, eigener Passkey oder Wiederherstellungscode. |
| TOTP | RFC 6238 (SHA-1, 30 s, 6 Stellen, ±1 Schritt), jeder Code nur einmal (letzter Schritt gespeichert). Schlüssel mit Master-Key verschlüsselt (AES-256-GCM, an Nutzer gebunden). QR-Code als SVG vom Server. |
| Wiederherstellung | 10 Einmal-Codes (gehasht), beim Aktivieren und auf Wunsch neu. Neue Codes und Abschalten brauchen das Passwort. Admins (Web, `phx admin reset-2fa`) entfernen bei Aussperrung alle Faktoren. |
| Passkeys | WebAuthn (go-webauthn), auffindbare Schlüssel. Anmeldung ohne Passwort nur mit Nutzerprüfung am Gerät; zusätzlich als zweiter Faktor. RP-ID = Host der öffentlichen URL. |
| Master-Key | `PHOEDOX_MASTER_KEY` oder `<data>/master.key` (wird angelegt, 0600). Schlüssel-ID im Chiffrat für spätere Rotation. Grundlage auch für Projekt-Geheimnisse. |

## SSO & OAuth für MCP

| Bereich | Entscheidung |
|---|---|
| OIDC | Ein Anbieter per Umgebung (`PHOEDOX_OIDC_*`), Discovery beim ersten Gebrauch. Code-Flow mit PKCE, Nonce, State an Cookie gebunden. Verknüpfung über (Issuer, Subject); bestehende Konten nur über bestätigte E-Mail. Neue Konten nur mit `AUTO_CREATE`, optional nur für bestimmte Domains. Zwei-Faktor übernimmt bei SSO der Anbieter. Mehrere Anbieter und Gruppen-Zuordnung kommen mit den Organisationen. |
| OAuth-Server | Eigener OAuth-2.1-Server nur für MCP: Metadaten (RFC 8414, RFC 9728), offene dynamische Registrierung (RFC 7591, Rate-Limit), Code + PKCE S256 Pflicht, `iss` im Redirect (RFC 9207), Resource `<base>/mcp` (RFC 8707). Redirects: https, Loopback (beliebiger Port) oder App-Schema. |
| Freigabe | Immer per Freigabeseite im Browser mit Sitzung (nicht per Token), auch bei bekannten Clients – Namen sind selbst angegeben. Eine Freigabe pro Nutzer und Client, widerrufbar unter „API-Tokens & MCP“. |
| Tokens | Zugriff 1 Stunde (`phxo_`, nur für `/mcp`), Refresh 30 Tage (`phxr_`), rotierend. Wiederverwendung eines Codes oder Refresh-Tokens widerruft die ganze Freigabe. |

## Suche

| Bereich | Entscheidung |
|---|---|
| Index | Eigene Tabelle `search_index` (Titel + reiner Text), gepflegt von der Geschäftslogik bei jedem Schreiben (auch angenommene KI-Vorschläge); beim ersten Start aus dem Bestand gefüllt, `phx admin reindex` baut neu. Markdown ohne Formatierung, Entscheidungen mit Nummer, Einträge mit Attributen. |
| Postgres | `tsvector` ('simple', Präfixe) + `pg_trgm` für Wortteile und Tippfehler im Titel; Rang: Volltext + Titelähnlichkeit. |
| SQLite | FTS5 mit Trigramm-Tokenizer (Wortteile, ohne Akzente); Wörter unter 3 Zeichen per LIKE. |
| Treffer | Alle Wörter müssen vorkommen; Fundstellen mit Markierung werden in Go erzeugt (beide Datenbanken gleich), die Web-App rendert sie ohne `innerHTML`. |
| Befehlspalette | ⌘K/Strg+K und Suchknopf: Aktionen, Projektwechsel, Treffer (aktuelles Projekt zuerst). Ohne Animation, da per Tastatur oft geöffnet. |

## Verlauf, Papierkorb, Favoriten

| Bereich | Entscheidung |
|---|---|
| Verlauf | Jede Version mit Autor (Name, „per KI“). Vergleich zeilenweise im Browser (Myers-Diff, eigene Umsetzung ohne Abhängigkeit), Entscheidungen als Text mit Abschnitten. Wiederherstellen speichert die alte Version als neue – über den normalen Änderungsweg (Vorschlagsmodus, Index, ADR-Verknüpfungen). MCP: `get_document_version`, `restore_document_version`. |
| Papierkorb | Gelöschte Dokumente und Einträge (mit ihren Beziehungen) bleiben `PHOEDOX_TRASH_DAYS` Tage (Standard 30) wiederherstellbar; „Rückgängig“ direkt nach dem Löschen. Bearbeiter stellen wieder her, nur Besitzer löschen endgültig. Ist der Name inzwischen vergeben, muss erst umbenannt werden. Slugs gelöschter Einträge sind frei (Migration 9, SQLite-Tabellenumbau). |
| Favoriten | Projekt-Favoriten (Pin, setzen Bearbeiter) und eigene (Stern) in einer leicht hervorgehobenen Gruppe oben in der Seitenleiste; Sortieren per Ziehen oder Alt+Pfeil. |

## Live-Bearbeitung

| Bereich | Entscheidung |
|---|---|
| Editor | Lexical ohne React (Rich Text, Listen, Links, Code, Markdown-Kürzel), Werkzeugleiste mit Tastenkürzeln; als eigenes Modul nachgeladen. Markdown-Seiten werden immer live bearbeitet, Entscheidungen und Links weiter per Formular. |
| Synchronisation | Yjs mit dem Standard-Client `y-websocket`, Server unter `/rt/doc/{projekt}/{dokument}` (nur Bearbeiter, Origin-Prüfung). Cursor und Anwesenheit über Awareness; Farben aus Tokens. |
| Server | **Abweichung vom Plan (yrs in WASM):** Der Go-Server führt Yjs-Updates nicht selbst zusammen. Er speichert das Update-Log (`doc_updates`), verteilt Updates und bittet bei wachsendem Log (200 Updates/2 MB) den sendenden Editor um einen zusammengefassten Stand (Kompaktierung). Ein leeres Live-Dokument befüllt genau ein vom Server bestimmter Editor aus dem Markdown. Eine Server-Instanz; für mehrere Instanzen später Verteilung über den Bus. |
| Inhalt | Markdown bleibt der maßgebliche Inhalt (Suche, MCP, Versionen, Vergleich). Wer tippt, speichert 1,5 s nach der letzten Eingabe den Stand als Markdown; nur echte Eingaben zählen. Speicherungen einer Person innerhalb von 10 Minuten ergeben eine Version; wer denselben Stand speichert, wird Mitautor. |
| Fremde Änderungen | Ändert sich der Inhalt außerhalb des Live-Editors (API, MCP, Wiederherstellen, angenommener Vorschlag, Löschen), wird das Log verworfen und alle Editoren laden neu (Hinweis mit Namen). |

## Request-Blöcke & Umgebung

| Bereich | Entscheidung |
|---|---|
| Format | Markdown-Codeblock ```` ```request ```` (oder `http`): Methode + URL, Header, Leerzeile, Body, Zeilen `expect …` (Status, `time`, `header X`, JSONPath-Teilmenge `$.a.b[0].c`, `.length`; `== != < <= > >= contains exists !exists`). Im Live-Editor ein normaler Codeblock, in der Ansicht interaktiv. |
| Umgebung | Pro Projekt Variablen (Bearbeiter) und Geheimnisse (Besitzer, AES-256-GCM mit Master-Key, an Projekt und Namen gebunden, nur schreibbar). Mehrere Umgebungen (Test/Produktion) später. |
| Ausführung | Nur gespeicherte Blöcke laufen (Dokument + Nummer), nie Text vom Browser – Leser können so keine eigenen Anfragen um Geheimnisse bauen. GET/HEAD/OPTIONS ab Leserecht, andere Methoden ab Bearbeiter mit Bestätigung, für KI gesperrt. Geheimnisse werden in URL, Headern, Body und Prüfwerten maskiert. 15 s, 1 MB, 3 Weiterleitungen, 30 Ausführungen/min pro Person; Adressprüfung nach DNS-Auflösung, Link-Local immer gesperrt, private Netze abschaltbar. MCP: `list_environment`, `run_request`. |

## KI-Zugriff

| Bereich | Entscheidung |
|---|---|
| Umfang | Tokens und OAuth-Freigaben tragen `access` (`read`/`write`) und eine Projektliste (leer = alle eigenen). Gilt zusätzlich zu den Rollen des Besitzers – nie mehr als er. Eingeschränkte Zugänge sehen fremde Projekte als „nicht gefunden“, legen keine Projekte an und erreichen keine Konto- oder Admin-Routen. |
| Projekt | KI-Modus `off` (Projekt für KI unsichtbar), `read`, `suggest` (Standard), `direct`. Nur Menschen ändern den Modus. |
| Abschalten | Schalter `ai_enabled` (Tabelle `instance_settings`): aus → `/mcp` antwortet 403, OAuth-Freigaben sind gesperrt. Der System-Admin-Schalter bleibt als Hauptschalter für die ganze Instanz; mit den Organisationen kommt zusätzlich ein Schalter pro Organisation für deren Admins (KI nur, wenn beide an sind). |

## Datenmodell & Wissensnetz

| Bereich | Entscheidung |
|---|---|
| Arten | Kern-Arten in `resources/kinds/core.json` (Allgemein, Gebäude, Netzwerk, Infrastruktur, Strom, Software) mit Attributen (`text`, `number` mit Einheit, `bool`, `date`, `ip`, `url`, `select`), Pflichtfeldern und erlaubten Beziehungen. Namen und Beschriftungen kommen aus den i18n-Tabellen (ein Bun-Test prüft, dass alle übersetzt sind). Projekte erweitern Kern-Arten um Attribute/Beziehungen oder legen eigene Arten an (Tabelle `project_kinds`, Symbol aus einer festen Lucide-Liste). |
| Prüfung | Einträge brauchen eine bekannte Art; bekannte Attribute werden geprüft und normalisiert (Zahlen aus Text, leere Werte entfallen), unbekannte bleiben erhalten. Beziehungen nur, wenn das Modell sie zwischen den Arten vorsieht; `related_to` und `depends_on` gehen immer. Ältere Einträge ohne Definition erscheinen als „Ohne Definition“. |
| KI | `get_data_model` liefert pro Art ein JSON-Schema und die Beziehungen; `define_kind` legt Arten an (im Vorschlagsmodus als Vorschlag). Löschen von Arten nur durch Menschen. |
| Oberfläche | Datenmodell-Seite (Arten, Schema-Graph, Einträge pro Art, Attribute/Beziehungen bearbeiten), Formular aus dem Schema, Eintragsseite mit direkten Nachbarn und „Verbinden“ (beide Richtungen, nur erlaubte Beziehungen), Wissensnetz-Ansicht mit Kraft-Layout (einmal berechnet, ohne Animation), Filter nach Art, Suche, Zoom/Verschieben. |

## Dateien

| Bereich | Entscheidung |
|---|---|
| Speicher | Inhalt einmal pro SHA-256 (`internal/blob`): lokal unter `<data>/files/ab/cd/<hash>` oder S3-kompatibel (`PHOEDOX_S3_*`, minio-go). Tabelle `files` hält Name, Typ, Größe, Projekt und Anhang (Dokument oder Eintrag). Inhalte werden erst gelöscht, wenn keine Zeile – auch im Papierkorb, auch in anderen Projekten – sie mehr nutzt. |
| Upload | Bis 8 MB ein Request (`POST /projects/{p}/files?name=&document=&entity=`), größere Dateien fortsetzbar nach **tus 1.0** (creation, termination; `POST /projects/{p}/uploads`, `HEAD`/`PATCH`/`DELETE /uploads/{id}`), eigener schlanker Server und Client in 4-MB-Stücken mit Wiederaufnahme nach Netzabbruch oder Neuladen. Grenze `PHOEDOX_MAX_UPLOAD_MB` (Standard 100). Unfertige Uploads verfallen nach 24 h. Bearbeiter laden hoch; KI per MCP nur `list_files`. |
| Prüfung | Virenscan vor dem Speichern mit ClamAV (`PHOEDOX_CLAMAV`, clamd INSTREAM). Ist clamd nicht erreichbar, wird abgelehnt (sicherer Ausfall). |
| Kontingente | Instanz-Admin setzt ein Maximum pro Projekt, Besitzer dürfen nur darunter bleiben (mit den Organisationen kommt die Organisationsebene dazu). Gezählt werden alle Versionen inklusive Papierkorb. |
| Versionen | Neue Version einer Datei per Upload oder tus (`file`-Metadatum); alte Versionen bleiben abrufbar (`?version=n`) und lassen sich wiederherstellen (als neue Version). |
| Vorschau & Text | Hintergrund-Job `file.process` (Job-Tabelle `jobs`, Postgres mit `FOR UPDATE SKIP LOCKED`, Wiederholung mit Backoff): Vorschaubild 480 px für Bilder (Go) und PDFs (pdftoppm), Text aus PDFs (pdftotext) und Textdateien, OCR für Bilder und gescannte PDFs (tesseract, `PHOEDOX_OCR_LANGS`). Der Text landet nur im Suchindex. Externe Werkzeuge laufen ohne Shell, mit Zeitlimit; das Image `-full` bringt poppler und tesseract mit, das Standard-Image bleibt minimal. |
| Typ & Auslieferung | Typ aus dem Inhalt (Sniffing), der Name nur bei allgemeinen Ergebnissen; die Angabe des Browsers zählt nie. Auslieferung unter `/api/v1/files/{id}/{name}` mit `nosniff`, CSP `sandbox` (außer PDF), `Cross-Origin-Resource-Policy: same-origin`, ETag = Hash, Range-Anfragen. Nur Bilder, PDF, Audio/Video und Text werden angezeigt, alles andere heruntergeladen. |
| Oberfläche | Anhänge an Seiten und Einträgen (Ziehen, Hochladen mit Fortschritt, Umbenennen, Papierkorb mit Rückgängig). Im Live-Editor Bilder einfügen, hineinziehen oder über die Werkzeugleiste: als eigener Absatz `![alt](/api/v1/files/…)`; andere Dateien als Link. Im Editor werden nur eigene Dateien und https-Bilder angezeigt. |
| Oberfläche | Vorschau-Dialog (Bild, PDF – ohne PDF-Viewer im Browser das Vorschaubild –, Video, Audio, Text) mit Versionen; Seite „Dateien“ pro Projekt; Speicheranzeige in den Projekteinstellungen; Datei-Treffer in der Suche öffnen die Vorschau. |

## Organisationen & Freigaben

| Bereich | Entscheidung |
|---|---|
| Organisationen | Legt der System-Admin an (mit Nutzungsart und erstem Admin). Org-Admins verwalten Mitglieder, Gruppen, den KI-Schalter und ein Speicherkontingent unterhalb der Instanz-Vorgabe; die Nutzungsart setzt nur der System-Admin. Org-Admins sind Besitzer aller Projekte ihrer Organisation; einfache Mitgliedschaft öffnet noch kein Projekt. Projekte sind persönlich oder gehören einer Organisation (verschiebbar durch Besitzer). |
| Gruppen | Pro Organisation; Rollen in Projekten der Organisation oder einzelne Seiten. Mit „OIDC-Gruppe“ füllt die SSO-Anmeldung die Gruppe (Claim `PHOEDOX_OIDC_GROUPS_CLAIM`, Standard `groups`) und macht den Nutzer zum Org-Mitglied; manuell hinzugefügte Mitglieder bleiben. |
| Gäste | Projektmitglieder mit Ablaufdatum (nicht als Besitzer); danach zählt die Mitgliedschaft nicht mehr. |
| Rechte | Effektive Rolle in SQL (beide Datenbanken): Mitgliedschaft, Gruppen, Org-Admin, und „doc“ für Personen, mit denen nur einzelne Seiten geteilt sind – sie sehen nur diese, keine Einträge, Dateien anderer Seiten oder Einstellungen. Seiten können eingeschränkt werden (nur Besitzer + Freigaben) und einzelnen Personen/Gruppen als Leser oder Bearbeiter freigegeben werden; das kann die Projektrolle auch erweitern. Gilt überall: Liste, Ansicht, Bearbeiten, Live-Editor, Verlauf, Request-Blöcke (zusätzlich Projekt-Leserecht wegen der Geheimnisse), Suche, Dateien der Seite. **Ordner-Ebene** folgt mit Kategorien als Ordnern. |
| Freigabe-Links | Nur lesen, ohne Konto: ganzes Projekt (Besitzer, ohne eingeschränkte Seiten) oder eine Seite (Bearbeiter; eingeschränkte nur Besitzer). Token `phxs_…` nur als Hash gespeichert, Ablauf wählbar, widerrufbar, letzte Nutzung sichtbar. Bilder der Seite laden über den Link, Erwähnungen und Request-Blöcke werden zu reinem Text. `noindex`, kein Referrer. Passwort und SSO für Links kommen mit den Veröffentlichungen. |

## Mandanten (gehostet)

| Bereich | Entscheidung |
|---|---|
| Modell | Ein Mandant = ein Kunde der gehosteten Variante mit eigenen Domains (`tenant_domains`), eigenen Nutzern (E-Mail eindeutig pro Mandant), Organisationen und Projekten. Selbst gehostet gibt es nur den Mandanten `default`. |
| Durchsetzung | Jede Tabelle hat `tenant_id`; Postgres-Row-Level-Security (`FORCE`) filtert auf `current_setting('phoedox.tenant')`. Der Server setzt den Wert: einmal je Verbindung (`default`) oder im gehosteten Modus (`PHOEDOX_MULTI_TENANT=true`) vor jeder Anweisung aus dem Host der Anfrage; ohne Mandant ist nichts sichtbar. Systemarbeit (Migrationen, Job-Abholung, Aufräumen, Neuaufbau des Suchindex) läuft mit `*`, jeder Job dann in seinem Mandanten. Abfragen filtern zusätzlich selbst – RLS ist die zweite Linie. |
| Schutz | Im gehosteten Modus startet der Server nicht als Superuser oder mit `BYPASSRLS`. Ein Test läuft mit einer Rolle ohne Sonderrechte und prüft Lesen, Schreiben und Einfügen über Mandantengrenzen. Dateiinhalte sind mandantenübergreifend nach Hash gespeichert; gelöscht wird erst, wenn kein Mandant sie mehr nutzt. |
| Verwaltung | `phx admin create-tenant --id … --name … --domain …`, `phx admin tenants`, Nutzerbefehle mit `--tenant`. Unbekannte Hosts bekommen 404. Datenmigrationen ab Version 18 beginnen mit `SELECT set_config('phoedox.tenant', '*', true);`. |

## Betrieb: Kubernetes auf einem Server

| Bereich | Entscheidung |
|---|---|
| Distribution | **k3s** für Single-Node-Kubernetes auf einem vServer: ein Binary, wenig Speicher, Traefik-Ingress, local-path-Storage und ServiceLB eingebaut, CNCF-zertifiziert, später um Knoten erweiterbar. Verglichen mit MicroK8s (Snap), k0s (ohne Ingress/Storage), Talos (eigenes OS-Image) und kubeadm (aufwendig). Anleitung: `docs/kubernetes.md`. |
| Paket | Helm-Chart `deploy/helm/phoedox`: Deployment (1 Replica, `Recreate`, nonroot, read-only Root-Dateisystem, `/tmp` als emptyDir), PVC, Service, Ingress (Traefik) mit cert-manager-ClusterIssuer für Let's Encrypt, Master-Key als einmal erzeugtes Secret (`resource-policy: keep`), optional ClamAV, Metriken mit ServiceMonitor, OIDC, SMTP, S3. Image-Variante `-full` als Standard. |
| Datenbank | CloudNativePG (`Cluster` mit Anwendungsnutzer als Eigentümer, kein Superuser → RLS wirkt), optional fortlaufende Sicherung nach S3 (barman) + täglicher `ScheduledBackup`; alternativ externe Datenbank oder SQLite. Der Server wartet beim Start bis zu zwei Minuten auf die Datenbank. |
| Gesundheit | `/healthz` (Prozess lebt) als Liveness, `/readyz` (Datenbank antwortet) als Startup/Readiness. |
| Prüfung | CI: `helm lint` und alle Wertevarianten gerendert und mit kubeconform gegen die Kubernetes-Schemas geprüft. |

## Betrieb: Jobs, Backups, Metriken, API, Webhooks

| Bereich | Entscheidung |
|---|---|
| Jobs | Tabelle `jobs`, abgeholt mit `FOR UPDATE SKIP LOCKED` (Postgres), Wiederholung mit exponentiellem Backoff (5 Versuche), `Permanent` für hoffnungslose Fehler, Zuteilung über Lease. `PHOEDOX_WORKERS` (0 = dieser Prozess arbeitet keine Jobs ab). Cron per Leader-Wahl folgt mit mehreren Instanzen. |
| Backups | Eigenes Format statt `pg_dump`: tar.gz mit Manifest, JSON-Lines pro Tabelle und Dateiinhalten; dadurch SQLite ↔ Postgres und Umzug zwischen Instanzen. Automatisch (`PHOEDOX_BACKUP_HOURS`, `_KEEP`), lokal oder im S3-Bucket, in der Oberfläche erstellen, herunterladen, hochladen und wiederherstellen (Bestätigung per Name, vorher automatisches Backup), CLI `phx admin backup/restore`. Der Master-Key ist nie Teil eines Backups; das Manifest merkt sich seine Kennung und warnt bei einem anderen Schlüssel. Suchindex wird nach dem Wiederherstellen neu aufgebaut. **Abweichung vom Plan:** SFTP als Ziel folgt später; im gehosteten Modus sichert der Betreiber die Datenbank (CloudNativePG). |
| Metriken | Prometheus unter `/metrics` mit Bearer-Token (`PHOEDOX_METRICS_TOKEN`): Anfragen nach Routenmuster (keine IDs/URLs in Labels), Dauer, Jobs nach Status, Live-Editoren, DB-Pool, Go-Laufzeit. OpenTelemetry folgt. |
| OpenAPI | `/api/v1/openapi.json` (3.1) aus dem Router und den Go-Typen (Schemas per Reflection); ein Test schlägt fehl, wenn eine Route nicht dokumentiert ist. |
| Webhooks | Pro Projekt (Besitzer): Ereignisse `document.*`, `entity.*`, `relation.*`, `file.*`, `suggestion.*`, Payload mit `version: 1`. Signatur `Phoedox-Signature: t=…,v1=HMAC-SHA256(t.body)`, Secret einmal angezeigt und mit dem Master-Key verschlüsselt. Zustellung über Jobs mit Wiederholungen, über den SSRF-geschützten Client ohne Weiterleitungen; Protokoll der letzten 50 Zustellungen, Test-Ping; nach 20 Fehlern in Folge wird der Webhook pausiert. |

## Plugins: Laufzeit, Pakete, Store

| Bereich | Entscheidung |
|---|---|
| Laufzeit | Extism auf wazero, kompiliert einmal pro Paket (Inhalts-Hash), **neue Instanz pro Aufruf** (kein geteilter Speicher zwischen Aufrufen). Grenzen: Speicher aus `backend.memoryMb` (Standard 64, max. 256 MB), 10 s pro Aufruf, höchstens 8 Aufrufe gleichzeitig. WASI nur mit Uhr und Zufall, kein Dateisystem, Extisms eigenes HTTP ist aus. Go-Plugins bauen als Reactor (`-buildmode=c-shared`, `//go:wasmexport`). |
| Host-API | **Eine** Host-Funktion `phx_host` (JSON `{op, args}` → `{ok, result, error, code}`), damit neue Operationen die ABI nicht brechen (`hostApi` "1"). Operationen: `graph.get/query/apply`, `secret.get`, `kv.get/set` (1000 Schlüssel, 256 KB je Wert pro Projekt), `http.fetch` (nur deklarierte Hosts, keine Weiterleitungen, 4 MB). Plugin-Exporte: `on_event`, `tool`. Go-Bindings im SDK (`pdk.Graph`, `pdk.Fetch`, `pdk.HandleEvent` …). |
| Rechte | Doppelt geprüft: zuerst gegen das Manifest (z. B. `graph:write:ups` nur für Art `ups`), dann läuft jede Änderung über die normalen Kernfunktionen mit den Rechten des Nutzers – bei Werkzeugen der Aufrufer (KI-Clients bleiben KI-Clients, „Vorschlag“-Modus gilt weiter), bei Zeitplan und Ereignissen der Nutzer, der das Plugin im Projekt eingeschaltet hat (verliert er den Zugang, läuft nichts). Actor `plugin:<nutzer>:<plugin>`, Quelle der Einträge `plugin`. Eigene Änderungen eines Plugins lösen bei ihm keine Ereignisse aus. |
| Pakete | `.phxp` = tar.gz mit `phoedox-plugin.json`, `backend.wasm`, Frontend-Modul, Schemas. Nur reguläre Dateien, keine Pfade außerhalb, 32 MB gepackt / 96 MB entpackt. Signatur ed25519 über `phoedox-plugin-v1\n<id>\n<version>\n<sha256>` (gleiche Bytes in Store und Server, Test-Vektor). Vertrauenswürdige Schlüssel: eingebaute Store-Schlüssel + `PHOEDOX_PLUGIN_KEYS`. Unsigniert nur mit `PHOEDOX_PLUGINS_ALLOW_UNSIGNED=true` (Entwicklung). Pakete liegen im Blob-Speicher und sind immer Teil von Backups. |
| Installation | Pro Mandant durch den (System-)Admin, Einschalten pro Projekt durch den Besitzer, Geheimnisse pro Projekt (mit dem Master-Key verschlüsselt, nie wieder angezeigt). Beiträge: Arten aus JSON Schema (oberste Eigenschaften → Attribute, `x-unit` für Einheiten, `enum` → Auswahl; Plugin-Arten sind im Projekt schreibgeschützt), Beziehungen, MCP-Werkzeuge (pro Mandant eigener MCP-Server, neu aufgebaut wenn sich Plugins ändern; Name `plugin_<tool>`, Pflichtargument `project`), Zeitplan (`schedule:every=60s`, mind. 30 s) und Ereignisse (`entity.*` …) als Jobs. |
| Frontend | ES-Modul pro Plugin-Version unter `/api/v1/plugins/<id>/<version>/files/…` (nur angemeldet, `nosniff`, unveränderlich cachebar). Die Web-App lädt die Module der Projekt-Plugins, injiziert das SDK (Graph, Werkzeuge, Toasts, Befehle in der Palette) und zeigt `views` auf Einträgen der Plugin-Arten. **Bewusst:** Frontend-Code läuft im Browser mit den Rechten des Nutzers; Schutz kommt aus Signatur und Moderation, das steht so auch in der Admin-Oberfläche. |
| Store-Anbindung | `PHOEDOX_STORE_URL` (Standard `https://store.phoedox.com`, `off`). Katalog gefiltert nach `engines.phoedox` und `hostApi`, Installation nach Bestätigung der Rechte, alle 6 h Abgleich: neue Versionen werden angezeigt, Rückrufe übernommen. Kritisch zurückgerufene Plugins laufen nicht mehr, außer `criticalPath` (dann Warnung). |
| Lizenzen | Lizenzdatei offline mit den Store-Schlüsseln geprüft, gebunden an Mandant (`orgId`) + Instanz-ID (pro Mandant zufällig, `instance_settings`). `license.paid` → ohne gültige Lizenz (oder Testphase ab Installation) nicht einschaltbar; `license.commercial` → nur Hinweis. Einlösen per Code über den Store oder Datei einfügen. |
| Store-Server | Eigenes Repo `phoedox-store`, nur PostgreSQL. Autoren-Token, erste Version reserviert die ID. Pakete werden **nie ausgeführt**: Analyse liest Manifest, Dateien, WASM-Import-/Export-Abschnitte und durchsucht Skripte nach Mustern (`eval`, Cookies, externe Requests …). Moderation im Browser (Basic-Auth mit Admin-Token, Same-Origin-Prüfung) mit Diff zur letzten freigegebenen Version; Freigeben signiert. Rückruf als Warnung oder kritisch (dann kein Download mehr). Gratis-Schlüssel im Monatskontingent, Autorenanteil nur über Admin-API. |
| Offen | Blöcke im Editor aus Plugins, Dokumenttypen, `docs:*`/`assets:*`/`ha:*`/`ai:*`-Operationen (Rechte sind im Manifest schon erlaubt, der Host lehnt sie noch ab), Bezahlung, Autorenportal im Browser, SDK als öffentliches Paket (`@phoedox/sdk`, Go-Modul getaggt). |

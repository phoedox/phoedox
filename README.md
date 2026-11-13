<p align="center"><img src="docs/logo.svg" width="96" alt="Phoedox-Logo"></p>

# Phoedox

Dokumentation, die weiß, wie alles zusammenhängt. Phoedox verbindet Texte, Grundrisse,
Serverschränke und Home Assistant zu einem Wissensnetz – steuerbar über die Web-App,
die Flutter-App und jede KI per MCP.

> Status: früh – Dokumente, Entscheidungen, Wissensnetz, Web-App und MCP laufen; Grundrisse, 3D, Plugins und Home Assistant folgen. Planung und Entscheidungen: [`docs/decisions.md`](docs/decisions.md).

## Installation

### Mit Docker Compose (PostgreSQL)

```bash
docker compose up -d
docker compose exec phoedox phx admin create-user --admin --email du@example.org --name "Dein Name"
```

Das Passwort wird interaktiv abgefragt. Die App läuft auf `127.0.0.1:8080`; davor gehört ein
Reverse-Proxy mit TLS, z. B. Caddy:

```
docs.example.org {
	reverse_proxy 127.0.0.1:8080
}
```

Für das gemeinsame Bearbeiten in Echtzeit (`/rt`) muss der Proxy WebSockets durchreichen – Caddy und
Traefik tun das von selbst, bei nginx `proxy_http_version 1.1` sowie die Header `Upgrade` und
`Connection "upgrade"` setzen. Live-Bearbeitung setzt derzeit **eine** Server-Instanz voraus.

Das Image `ghcr.io/phoedox/phoedox` wird bei jedem Push auf `main` (`:main`) und bei jedem
Release (`:X.Y.Z`, `:latest`) gebaut. Solange das Repo privat ist, vorher `docker login ghcr.io`
mit einem Token mit `read:packages`. Jedes Tag gibt es auch mit `-full` (z. B. `:main-full`): darin
sind poppler und tesseract für PDF-Vorschauen, durchsuchbare PDFs und Texterkennung in Scans. Für den
Virenscan einen ClamAV-Container (`clamav/clamav`) danebenstellen und `PHOEDOX_CLAMAV=clamav:3310` setzen.

### Nur ein Container (SQLite)

```bash
docker run -d --name phoedox -p 127.0.0.1:8080:8080 -v phoedox-data:/data ghcr.io/phoedox/phoedox:main
docker exec -it phoedox phx admin create-user --admin --email du@example.org --name "Dein Name"
```

### Ohne Docker

```bash
make build && ./bin/phoedox
```

### Konfiguration

| Variable | Standard | Bedeutung |
|---|---|---|
| `PHOEDOX_LISTEN` | `:8080` | HTTP-Adresse |
| `PHOEDOX_DOMAIN` | – | öffentliche Adresse der Instanz |
| `PHOEDOX_DATA_DIR` | `./data` (Image: `/data`) | Dateien und SQLite-Datenbank |
| `PHOEDOX_DB_DRIVER` | `sqlite` | `sqlite` oder `postgres` |
| `PHOEDOX_DB_URL` | `<data>/phoedox.db` | SQLite-Pfad oder Postgres-DSN |
| `PHOEDOX_BEHIND_PROXY` | `false` | `true` hinter einem Reverse-Proxy: Client-IP aus `X-Forwarded-For` (für das Login-Limit) |
| `PHOEDOX_INSECURE_COOKIES` | `false` | `true` nur für reines HTTP ohne TLS (außer `localhost`) |
| `PHOEDOX_REQUESTS_PRIVATE` | `true` (`false` bei `PHOEDOX_MULTI_TENANT`) | Request-Blöcke dürfen private Netze und `localhost` erreichen (Heim-IT). Link-Local-Adressen wie Cloud-Metadaten sind immer gesperrt |
| `PHOEDOX_TRASH_DAYS` | `30` | So lange bleiben gelöschte Dokumente und Einträge wiederherstellbar (`0` = unbegrenzt) |
| `PHOEDOX_MASTER_KEY` | – | 32 Byte, base64 (`openssl rand -base64 32`). Verschlüsselt Geheimnisse wie TOTP-Schlüssel. Ohne Variable legt Phoedox `<data>/master.key` an – **zusammen mit der Datenbank sichern**, mehrere Server-Instanzen brauchen denselben Schlüssel |
| `PHOEDOX_PUBLIC_URL` | – | Öffentliche Adresse, z. B. `https://docs.example.org` – für Links in E-Mails. Ohne sie (oder `PHOEDOX_DOMAIN`) gibt es kein „Passwort vergessen“ per E-Mail |
| `PHOEDOX_DEFAULT_LOCALE` | `de` | Sprache für E-Mails an Nutzer ohne eigene Spracheinstellung |
| `PHOEDOX_OIDC_ISSUER` | – | Anmeldung über einen OpenID-Connect-Anbieter (Authentik, Keycloak, Entra ID, Google …), z. B. `https://auth.example.org/application/o/phoedox/`. Redirect-URI beim Anbieter: `<öffentliche URL>/api/v1/auth/oidc/callback` |
| `PHOEDOX_OIDC_CLIENT_ID` / `PHOEDOX_OIDC_CLIENT_SECRET` | – | Zugangsdaten beim Anbieter |
| `PHOEDOX_OIDC_NAME` | `SSO` | Beschriftung des Knopfs („Mit … anmelden“) |
| `PHOEDOX_OIDC_SCOPES` | `openid email profile` | |
| `PHOEDOX_OIDC_AUTO_CREATE` | `false` | `true`: Konten beim ersten Anmelden anlegen, sonst nur für eingeladene Personen |
| `PHOEDOX_OIDC_DOMAINS` | – | Nur diese E-Mail-Domains dürfen automatisch angelegt werden (Komma-Liste) |
| `PHOEDOX_OIDC_TRUST_EMAIL` | `false` | E-Mails als bestätigt behandeln, wenn der Anbieter kein `email_verified` sendet (z. B. Entra ID) |
| `PHOEDOX_SMTP_HOST` | – | SMTP-Server; aktiviert Einladungen und „Passwort vergessen“ per E-Mail |
| `PHOEDOX_SMTP_PORT` | `587` (`465` bei `tls`) | |
| `PHOEDOX_SMTP_TLS` | `starttls` | `starttls`, `tls` oder `none` (nur lokales Relay) |
| `PHOEDOX_SMTP_USER` / `PHOEDOX_SMTP_PASSWORD` | – | Anmeldung am SMTP-Server |
| `PHOEDOX_SMTP_FROM` | – | Absender, z. B. `Phoedox <noreply@example.org>` (Pflicht mit SMTP) |
| `PHOEDOX_MAX_UPLOAD_MB` | `100` | Größte Datei pro Upload (größere Dateien laden fortsetzbar per tus hoch) |
| `PHOEDOX_CLAMAV` | – | clamd-Adresse für den Virenscan, z. B. `clamav:3310` oder `unix:/run/clamav/clamd.ctl`; ohne Antwort werden Uploads abgelehnt |
| `PHOEDOX_OCR`, `PHOEDOX_OCR_LANGS` | `true`, `deu+eng` | Texterkennung in Bildern und Scans, wenn tesseract installiert ist |
| `PHOEDOX_MULTI_TENANT` | `false` | Gehosteter Modus: mehrere Kunden (Mandanten) in einer Postgres-Datenbank, getrennt per Row-Level-Security; Mandant aus der Domain (`phx admin create-tenant`). Der DB-Nutzer darf kein Superuser sein. |
| `PHOEDOX_WORKERS` | `2` | Parallele Hintergrund-Jobs (Vorschauen, Text); `0` = keine in diesem Prozess |
| `PHOEDOX_S3_ENDPOINT`, `_BUCKET`, `_REGION`, `_ACCESS_KEY`, `_SECRET_KEY`, `_PREFIX` | – | Dateien in einem S3-kompatiblen Bucket statt unter `<data>/files` |
| `PHOEDOX_PROMPTS_DIR` | – | Ordner mit eigenen KI-Prompts, siehe [`resources/prompts`](resources/prompts) |
| `PHOEDOX_MAIL_DIR` | – | Ordner mit eigenen E-Mail-Vorlagen, siehe [`resources/mail`](resources/mail) |

Weitere Personen lädst du in der Web-App unter **Nutzer** ein (Menü oben rechts) oder mit
`phx admin invite --email … --name …`. Ohne SMTP zeigt Phoedox den Einladungslink zum Weitergeben an.

Die Suche nutzt in PostgreSQL die Erweiterung `pg_trgm`. Phoedox legt sie beim ersten Start an; bei
verwalteten Datenbanken ohne diese Rechte einmalig `CREATE EXTENSION pg_trgm;` ausführen.
`phx admin reindex` baut den Suchindex neu auf.

Notfall-Befehle laufen auch ohne Web-Oberfläche: `phx admin reset-password --email …`,
`phx admin reset-2fa --email …` (Zwei-Faktor und Passkeys entfernen), `phx admin create-token --email … --name …`.

Passkeys funktionieren nur über HTTPS (oder `localhost`) und sind an die Domain gebunden – mit
`PHOEDOX_PUBLIC_URL` legst du sie fest.

## KI per MCP anbinden

**Mit OAuth** (z. B. Claude als eigener Connector): nur die Adresse `https://docs.example.org/mcp`
eintragen. Der Client registriert sich selbst, du meldest dich in Phoedox an und erlaubst den Zugriff.
Verbundene Apps siehst und widerrufst du unter **API-Tokens & MCP**.

**Mit Token** (Clients ohne OAuth):

1. In der Web-App unter **API-Tokens & MCP** ein Token erzeugen (oder `phx admin create-token`).
2. Den MCP-Server eintragen, z. B. für Claude Code:

   ```bash
   claude mcp add --transport http phoedox https://docs.example.org/mcp --header "Authorization: Bearer phx_…"
   ```

Die KI arbeitet höchstens mit den Rechten des Token-Besitzers. Beim Erzeugen eines Tokens bzw. bei
der Freigabe einer App wählst du **Nur lesen** oder **Lesen und schreiben** und **alle Projekte** oder
**nur ausgewählte**. Pro Projekt legt der KI-Modus fest, was KI dort darf: **Kein Zugriff**, **Nur
lesen**, **Vorschlagen** (Standard – Änderungen landen als Vorschläge, die ein Mensch annimmt) oder
**Direkt**. Admins können KI-Zugriff unter **Einstellungen → Instanz** komplett abschalten.

Werkzeuge: Projekte auflisten und anlegen, Projektüberblick, Dokumente (Markdown, Entscheidungen/ADRs,
externe Links) lesen, anlegen, ändern und versionieren, das Datenmodell lesen und erweitern, Einträge
und Beziehungen im Wissensnetz pflegen und durchsuchen.

## Repositories

| Repo | Inhalt |
|---|---|
| [phoedox](https://github.com/phoedox/phoedox) | Server + Web-App (dieses Repo) |
| [phoedox-sdk](https://github.com/phoedox/phoedox-sdk) | Plugin-SDK für Go und TypeScript |
| [phoedox-plugins](https://github.com/phoedox/phoedox-plugins) | Offizielle Plugins |
| [phoedox-agent](https://github.com/phoedox/phoedox-agent) | Agent für Server, VMs und LXC |
| [phoedox-store](https://github.com/phoedox/phoedox-store) | Plugin-Store |
| [phoedox-app](https://github.com/phoedox/phoedox-app) | Flutter-App |

## Lizenz

AGPL-3.0 – siehe [LICENSE](LICENSE).

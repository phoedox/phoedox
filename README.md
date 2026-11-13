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

Das Image `ghcr.io/phoedox/phoedox` wird bei jedem Push auf `main` (`:main`) und bei jedem
Release (`:X.Y.Z`, `:latest`) gebaut. Solange das Repo privat ist, vorher `docker login ghcr.io`
mit einem Token mit `read:packages`.

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
| `PHOEDOX_MASTER_KEY` | – | 32 Byte, base64 (`openssl rand -base64 32`). Verschlüsselt Geheimnisse wie TOTP-Schlüssel. Ohne Variable legt Phoedox `<data>/master.key` an – **zusammen mit der Datenbank sichern**, mehrere Server-Instanzen brauchen denselben Schlüssel |
| `PHOEDOX_PUBLIC_URL` | – | Öffentliche Adresse, z. B. `https://docs.example.org` – für Links in E-Mails. Ohne sie (oder `PHOEDOX_DOMAIN`) gibt es kein „Passwort vergessen“ per E-Mail |
| `PHOEDOX_DEFAULT_LOCALE` | `de` | Sprache für E-Mails an Nutzer ohne eigene Spracheinstellung |
| `PHOEDOX_SMTP_HOST` | – | SMTP-Server; aktiviert Einladungen und „Passwort vergessen“ per E-Mail |
| `PHOEDOX_SMTP_PORT` | `587` (`465` bei `tls`) | |
| `PHOEDOX_SMTP_TLS` | `starttls` | `starttls`, `tls` oder `none` (nur lokales Relay) |
| `PHOEDOX_SMTP_USER` / `PHOEDOX_SMTP_PASSWORD` | – | Anmeldung am SMTP-Server |
| `PHOEDOX_SMTP_FROM` | – | Absender, z. B. `Phoedox <noreply@example.org>` (Pflicht mit SMTP) |
| `PHOEDOX_PROMPTS_DIR` | – | Ordner mit eigenen KI-Prompts, siehe [`resources/prompts`](resources/prompts) |
| `PHOEDOX_MAIL_DIR` | – | Ordner mit eigenen E-Mail-Vorlagen, siehe [`resources/mail`](resources/mail) |

Weitere Personen lädst du in der Web-App unter **Nutzer** ein (Menü oben rechts) oder mit
`phx admin invite --email … --name …`. Ohne SMTP zeigt Phoedox den Einladungslink zum Weitergeben an.

Notfall-Befehle laufen auch ohne Web-Oberfläche: `phx admin reset-password --email …`,
`phx admin reset-2fa --email …` (Zwei-Faktor und Passkeys entfernen), `phx admin create-token --email … --name …`.

Passkeys funktionieren nur über HTTPS (oder `localhost`) und sind an die Domain gebunden – mit
`PHOEDOX_PUBLIC_URL` legst du sie fest.

## KI per MCP anbinden

1. In der Web-App unter **API-Tokens & MCP** ein Token erzeugen (oder `phx admin create-token`).
2. Den MCP-Server eintragen, z. B. für Claude Code:

   ```bash
   claude mcp add --transport http phoedox https://docs.example.org/mcp --header "Authorization: Bearer phx_…"
   ```

Die KI arbeitet mit den Rechten des Token-Besitzers. In Projekten mit KI-Modus **Vorschlagen**
(Standard) landen ihre Änderungen als Vorschläge, die ein Mensch annimmt; mit **Direkt** schreibt sie
sofort. Umschalten in den Projekteinstellungen.

Werkzeuge: Projekte auflisten und anlegen, Projektüberblick, Dokumente (Markdown, Entscheidungen/ADRs,
externe Links) lesen, anlegen, ändern und versionieren, Einträge und Beziehungen im Wissensnetz
pflegen und durchsuchen.

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

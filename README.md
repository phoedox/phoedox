<p align="center"><img src="docs/logo.svg" width="96" alt="Phoedox logo"></p>

# Phoedox

Documentation that knows how everything fits together. Phoedox connects texts, floor plans,
server racks and Home Assistant into a knowledge graph – controllable through the web app,
the Flutter app and any AI via MCP.

> Status: early – documents, decisions, knowledge graph, web app and MCP work; floor plans, 3D, plugins and Home Assistant will follow. Decisions are kept in Phoedox itself (project `phoedox`, see [`docs/decisions.md`](docs/decisions.md)).

## Installation

### With Docker Compose (PostgreSQL)

```bash
docker compose up -d
docker compose exec phoedox phx admin create-user --admin --email du@example.org --name "Dein Name"
```

The password is asked for interactively. The app runs on `127.0.0.1:8080`; put a
reverse proxy with TLS in front of it, e.g. Caddy:

```
docs.example.org {
	reverse_proxy 127.0.0.1:8080
}
```

For real-time collaborative editing (`/rt`), the proxy must pass WebSockets through. Caddy and
Traefik do this on their own; with nginx, set `proxy_http_version 1.1` and the headers `Upgrade` and
`Connection "upgrade"`. Live editing currently requires **one** server instance.

The image `ghcr.io/phoedox/phoedox` is built on every push to `main` (`:main`) and on every
release (`:X.Y.Z`, `:latest`). As long as the repo is private, first run `docker login ghcr.io`
with a token that has `read:packages`. Every tag also exists with `-full` (e.g. `:main-full`): it
contains poppler and tesseract for PDF previews, searchable PDFs and text recognition in scans. For
virus scanning, run a ClamAV container (`clamav/clamav`) next to it and set `PHOEDOX_CLAMAV=clamav:3310`.

### Single container (SQLite)

```bash
docker run -d --name phoedox -p 127.0.0.1:8080:8080 -v phoedox-data:/data ghcr.io/phoedox/phoedox:main
docker exec -it phoedox phx admin create-user --admin --email du@example.org --name "Dein Name"
```

### Kubernetes (k3s on one server)

Helm chart in [`deploy/helm/phoedox`](deploy/helm/phoedox), step-by-step guide with k3s,
CloudNativePG and cert-manager in [`docs/kubernetes.md`](docs/kubernetes.md).

### Without Docker

```bash
make build && ./bin/phoedox
```

### Configuration

| Variable | Default | Meaning |
|---|---|---|
| `PHOEDOX_LISTEN` | `:8080` | HTTP address |
| `PHOEDOX_DOMAIN` | – | public address of the instance |
| `PHOEDOX_DATA_DIR` | `./data` (image: `/data`) | files and SQLite database |
| `PHOEDOX_DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `PHOEDOX_DB_URL` | `<data>/phoedox.db` | SQLite path or Postgres DSN |
| `PHOEDOX_BEHIND_PROXY` | `false` | `true` behind a reverse proxy: client IP from `X-Forwarded-For` (for the login limit) |
| `PHOEDOX_INSECURE_COOKIES` | `false` | `true` only for plain HTTP without TLS (except `localhost`) |
| `PHOEDOX_REQUESTS_PRIVATE` | `true` (`false` with `PHOEDOX_MULTI_TENANT`) | Request blocks may reach private networks and `localhost` (home IT). Link-local addresses such as cloud metadata are always blocked |
| `PHOEDOX_TRASH_DAYS` | `30` | How long deleted documents and entries stay restorable (`0` = unlimited) |
| `PHOEDOX_MASTER_KEY` | – | 32 bytes, base64 (`openssl rand -base64 32`). Encrypts secrets such as TOTP keys. Without the variable, Phoedox creates `<data>/master.key` – **back it up together with the database**; multiple server instances need the same key |
| `PHOEDOX_PUBLIC_URL` | – | Public address, e.g. `https://docs.example.org` – for links in emails. Without it (or `PHOEDOX_DOMAIN`), there is no "forgot password" by email |
| `PHOEDOX_DEFAULT_LOCALE` | `de` | Language for emails to users without their own language setting |
| `PHOEDOX_OIDC_ISSUER` | – | Sign-in through an OpenID Connect provider (Authentik, Keycloak, Entra ID, Google …), e.g. `https://auth.example.org/application/o/phoedox/`. Redirect URI at the provider: `<public URL>/api/v1/auth/oidc/callback` |
| `PHOEDOX_OIDC_CLIENT_ID` / `PHOEDOX_OIDC_CLIENT_SECRET` | – | Credentials at the provider |
| `PHOEDOX_OIDC_NAME` | `SSO` | Label of the button ("Sign in with …") |
| `PHOEDOX_OIDC_SCOPES` | `openid email profile` | |
| `PHOEDOX_OIDC_AUTO_CREATE` | `false` | `true`: create accounts on first sign-in; otherwise only for invited people |
| `PHOEDOX_OIDC_DOMAINS` | – | Only these email domains may be created automatically (comma-separated list) |
| `PHOEDOX_OIDC_TRUST_EMAIL` | `false` | Treat emails as verified if the provider does not send `email_verified` (e.g. Entra ID) |
| `PHOEDOX_SMTP_HOST` | – | SMTP server; enables invitations and "forgot password" by email |
| `PHOEDOX_SMTP_PORT` | `587` (`465` with `tls`) | |
| `PHOEDOX_SMTP_TLS` | `starttls` | `starttls`, `tls` or `none` (local relay only) |
| `PHOEDOX_SMTP_USER` / `PHOEDOX_SMTP_PASSWORD` | – | Sign-in at the SMTP server |
| `PHOEDOX_SMTP_FROM` | – | Sender, e.g. `Phoedox <noreply@example.org>` (required with SMTP) |
| `PHOEDOX_MAX_UPLOAD_MB` | `100` | Largest file per upload (larger files upload resumably via tus) |
| `PHOEDOX_CLAMAV` | – | clamd address for virus scanning, e.g. `clamav:3310` or `unix:/run/clamav/clamd.ctl`; without a response, uploads are rejected |
| `PHOEDOX_OCR`, `PHOEDOX_OCR_LANGS` | `true`, `deu+eng` | Text recognition in images and scans, if tesseract is installed |
| `PHOEDOX_BACKUP_HOURS`, `PHOEDOX_BACKUP_KEEP` | `24`, `14` | Automatic backups (0 = off) and how many are kept |
| `PHOEDOX_BACKUP_TARGET`, `PHOEDOX_BACKUP_DIR`, `PHOEDOX_BACKUP_FILES` | `local`, `<data>/backups`, `true` | Target (`local` or `s3` in the file bucket), folder, also back up file contents |
| `PHOEDOX_METRICS_TOKEN` | – | Enables `/metrics` (Prometheus) with this bearer token |
| `PHOEDOX_MULTI_TENANT` | `false` | Hosted mode: multiple customers (tenants) in one Postgres database, separated by Row-Level Security; tenant from the domain (`phx admin create-tenant`). The DB user must not be a superuser. |
| `PHOEDOX_PLUGINS` | `true` | Run WASM plugins |
| `PHOEDOX_STORE_URL` | `https://store.phoedox.com` | Plugin store; `off` disables it |
| `PHOEDOX_PLUGIN_KEYS` | – | Additional trusted signing keys `id:base64,…` (e.g. your own store) |
| `PHOEDOX_PLUGINS_ALLOW_UNSIGNED` | `false` | Allow unsigned packages – only for plugin development |
| `PHOEDOX_WORKERS` | `2` | Parallel background jobs (previews, text); `0` = none in this process |
| `PHOEDOX_S3_ENDPOINT`, `_BUCKET`, `_REGION`, `_ACCESS_KEY`, `_SECRET_KEY`, `_PREFIX` | – | Files in an S3-compatible bucket instead of under `<data>/files` |
| `PHOEDOX_PROMPTS_DIR` | – | Folder with your own AI prompts, see [`resources/prompts`](resources/prompts) |
| `PHOEDOX_MAIL_DIR` | – | Folder with your own email templates, see [`resources/mail`](resources/mail) |

You invite more people in the web app under **Users** (menu at the top right) or with
`phx admin invite --email … --name …`. Without SMTP, Phoedox shows the invitation link for you to pass on.

In PostgreSQL, search uses the extension `pg_trgm`. Phoedox creates it on first start; on
managed databases without these permissions, run `CREATE EXTENSION pg_trgm;` once.
`phx admin reindex` rebuilds the search index.

Emergency commands also work without the web interface: `phx admin reset-password --email …`,
`phx admin reset-2fa --email …` (removes two-factor and passkeys), `phx admin create-token --email … --name …`.

Passkeys only work over HTTPS (or `localhost`) and are bound to the domain – you set it
with `PHOEDOX_PUBLIC_URL`.

## Connecting AI via MCP

**With OAuth** (e.g. Claude as a custom connector): just enter the address `https://docs.example.org/mcp`.
The client registers itself; you sign in to Phoedox and allow access.
You see and revoke connected apps under **API tokens & MCP**.

**With a token** (clients without OAuth):

1. In the web app, create a token under **API tokens & MCP** (or `phx admin create-token`).
2. Add the MCP server, e.g. for Claude Code:

   ```bash
   claude mcp add --transport http phoedox https://docs.example.org/mcp --header "Authorization: Bearer phx_…"
   ```

The AI works with at most the permissions of the token owner. When you create a token or
approve an app, you choose **Read only** or **Read and write**, and **all projects** or
**only selected ones**. Per project, the AI mode sets what AI may do there: **No access**, **Read
only**, **Suggest** (default – changes arrive as suggestions that a human accepts) or
**Direct**. Admins can turn off AI access completely under **Settings → Instance**.

Tools: list and create projects, project overview; read, create, change and version documents (Markdown, decisions/ADRs,
external links); read and extend the data model; maintain and search entries
and relations in the knowledge graph.

## Repositories

| Repo | Content |
|---|---|
| [phoedox](https://github.com/phoedox/phoedox) | Server + web app (this repo) |
| [phoedox-sdk](https://github.com/phoedox/phoedox-sdk) | Plugin SDK for Go and TypeScript |
| [phoedox-plugins](https://github.com/phoedox/phoedox-plugins) | Official plugins |
| [phoedox-agent](https://github.com/phoedox/phoedox-agent) | Agent for servers, VMs and LXC |
| [phoedox-store](https://github.com/phoedox/phoedox-store) | Plugin store |
| [phoedox-app](https://github.com/phoedox/phoedox-app) | Flutter app |

## License

AGPL-3.0 – see [LICENSE](LICENSE).

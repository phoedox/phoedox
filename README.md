<p align="center"><img src="docs/logo.svg" width="96" alt="Phoedox logo"></p>

# Phoedox

Documentation that knows how everything fits together. Phoedox connects texts, floor plans,
server racks and Home Assistant into a knowledge graph – controllable through the web app,
the Flutter app and any AI via MCP.

> Status: early – documents, decisions, knowledge graph, task boards, floor plans (live editing, DXF/IFC/RoomPlan import), plugins, agents, web app and MCP work; 3D views and more integrations will follow. Decisions are kept in Phoedox itself (project `phoedox`, see [`docs/decisions.md`](docs/decisions.md)).

## Installation

### With Docker Compose (PostgreSQL)

```bash
docker compose up -d
docker compose exec phoedox phx admin create-user --admin --email you@example.org --name "Your Name"
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
docker exec -it phoedox phx admin create-user --admin --email you@example.org --name "Your Name"
```

### Kubernetes (k3s on one server)

Helm chart in [`deploy/helm/phoedox`](deploy/helm/phoedox), step-by-step guide with k3s,
CloudNativePG and cert-manager in [`docs/kubernetes.md`](docs/kubernetes.md).

### Home Assistant add-on

Add the repository `https://github.com/phoedox/home-assistant-addon` under **Settings → Add-ons →
Add-on store → ⋮ → Repositories** and install **Phoedox**. Data stays in the add-on (part of Home
Assistant backups), people sign in with their Home Assistant users. The add-on files live in
[`deploy/homeassistant`](deploy/homeassistant); the add-on repository follows each Phoedox release
automatically. Setup in [`DOCS.md`](deploy/homeassistant/phoedox/DOCS.md).
Ask your documentation by voice with Assist: [`docs/home-assistant-assist.md`](docs/home-assistant-assist.md).

### Without Docker

```bash
make build && ./bin/phoedox
```

### Configuration

| Variable | Default | Meaning |
|---|---|---|
| `PHOEDOX_LISTEN` | `:8080` | HTTP address |
| `PHOEDOX_USERCONTENT_LISTEN` | `:8443` | HTTPS address of the user content domain apps run on (domain, DNS provider and wildcard certificate are set in Administration → User content); `off` disables it |
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
| `PHOEDOX_PAID_FEATURES` | – | Paid features with their start date, e.g. `custom_domains=2027-03-01` (ADR-0061). Accounts and organizations created before the date keep the feature for free; `phx admin entitle` grants it to others. Empty = everything free |
| `PHOEDOX_SIGNUP` | `invite` | `open` lets anyone create an account with a confirmed e-mail address (needs mail); admins can switch it under Admin → Instance |
| `PHOEDOX_IDENTITY_CLIENTS` | – | Services that may sign people in with their Phoedox account and learn only id, name and e-mail, e.g. `phoedox-store\|Phoedox Store\|https://store.phoedox.com/auth/callback` (comma-separated) |
| `PHOEDOX_TERMS_URL`, `PHOEDOX_PRIVACY_URL` | – | Terms of use and privacy policy shown on the sign-up page; when set, people must accept them |
| `PHOEDOX_OIDC_ISSUER` | – | Sign-in through an OpenID Connect provider (Authentik, Keycloak, Entra ID, Google …), e.g. `https://auth.example.org/application/o/phoedox/`. Redirect URI at the provider: `<public URL>/api/v1/auth/oidc/callback` |
| `PHOEDOX_HA_URL` | – | Sign-in with Home Assistant users: the address browsers use for Home Assistant, e.g. `http://homeassistant.local:8123`. Phoedox's public URL is the client ID. |
| `PHOEDOX_HA_INTERNAL_URL` | `PHOEDOX_HA_URL` | Address the server uses for Home Assistant (token and WebSocket), e.g. `http://homeassistant:8123` in the add-on |
| `PHOEDOX_HA_AUTO_CREATE` | `true` | Create accounts on first sign-in with Home Assistant; the HA owner becomes a system admin |
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
| `PHOEDOX_SMTP_HELO` | domain | Name sent in EHLO; servers may reject `localhost` |
| `PHOEDOX_SMTP_CA_FILE` | – | Extra CA certificates (PEM) for a mail server with an internal or self-signed certificate |
| `PHOEDOX_MAX_UPLOAD_MB` | `100` | Largest file per upload (larger files upload resumably via tus) |
| `PHOEDOX_CLAMAV` | – | clamd address for virus scanning, e.g. `clamav:3310` or `unix:/run/clamav/clamd.ctl`; without a response, uploads are rejected |
| `PHOEDOX_OCR`, `PHOEDOX_OCR_LANGS` | `true`, `deu+eng` | Text recognition in images and scans, if tesseract is installed |
| `PHOEDOX_BACKUP_HOURS`, `PHOEDOX_BACKUP_KEEP` | `24`, `14` | Automatic backups (0 = off) and how many are kept |
| `PHOEDOX_BACKUP_TARGET`, `PHOEDOX_BACKUP_DIR`, `PHOEDOX_BACKUP_FILES` | `local`, `<data>/backups`, `true` | Target (`local` or `s3` in the file bucket), folder, also back up file contents |
| `PHOEDOX_METRICS_TOKEN` | – | Enables `/metrics` (Prometheus) with this bearer token |
| `PHOEDOX_MULTI_TENANT` | `false` | Hosted mode: multiple customers (tenants) in one Postgres database, separated by Row-Level Security; tenant from the domain (`phx admin create-tenant`). The DB user must not be a superuser. |
| `PHOEDOX_SINGLE_ORG` | `false` | Single-organization edition (ADR-0092): the instance runs for one organization. It is created at the first sign-in (that person becomes its admin); every account joins it (system admins as admins), new projects belong to it, no further organizations can be created. Projects that existed before stay where they are. Not with `PHOEDOX_MULTI_TENANT`. Helm: `singleOrg.enabled` |
| `PHOEDOX_ORG_NAME` | `Phoedox` | Name of that organization when it is created (renamed later in its settings). Helm: `singleOrg.name` |
| `PHOEDOX_PLUGINS` | `true` | Run WASM plugins |
| `PHOEDOX_STORE_URL` | `https://store.phoedox.com` | Plugin store; `off` disables it |
| `PHOEDOX_PLUGIN_KEYS` | – | Additional trusted signing keys `id:base64,…` (e.g. your own store) |
| `PHOEDOX_PLUGINS_ALLOW_UNSIGNED` | `false` | Allow unsigned packages – only for plugin development |
| `PHOEDOX_WORKERS` | `2` | Parallel background jobs (previews, text); `0` = none in this process |
| `PHOEDOX_S3_ENDPOINT`, `_BUCKET`, `_REGION`, `_ACCESS_KEY`, `_SECRET_KEY`, `_PREFIX` | – | Files in an S3-compatible bucket instead of under `<data>/files` |
| `PHOEDOX_AI_PROVIDER`, `PHOEDOX_AI_API_KEY` | – | Optional built-in model provider of the AI chat (ADR-0066): `anthropic` and an API key. Usually providers are set up in the app instead (ADR-0067): Admin → Instance → AI chat → Models for everyone, the organization page for an organization's own keys. Anthropic or any OpenAI-compatible API (OpenAI, Mistral, Kimi, OpenRouter, Ollama, own gateways); switches and monthly budgets are there too |
| `PHOEDOX_AI_MODEL`, `PHOEDOX_AI_EFFORT` | `claude-opus-5-5`, `medium` | Model and effort (`low` … `max`) of the AI chat |
| `PHOEDOX_AI_BASE_URL` | – | Another address for the provider's API, e.g. a gateway |
| `PHOEDOX_AI_PRICE` | – | Prices for budgets in US dollars per million tokens, `input,output,cache read,cache write`; only needed for models or gateways Phoedox does not know |
| `PHOEDOX_PROMPTS_DIR` | – | Folder with your own AI prompts, see [`resources/prompts`](resources/prompts) |
| `PHOEDOX_MAIL_DIR` | – | Folder with your own email templates (`<name>.<lang>.md`) and HTML frame (`layout.html`), see [`resources/mail`](resources/mail). Mails are sent as text plus HTML in the theme of the organization or instance |

You invite more people in the web app under **Users** (menu at the top right) or with
`phx admin invite --email … --name …`. Without SMTP, Phoedox shows the invitation link for you to pass on.

In PostgreSQL, search uses the extension `pg_trgm`. Phoedox creates it on first start; on
managed databases without these permissions, run `CREATE EXTENSION pg_trgm;` once.
`phx admin reindex` rebuilds the search index.

Emergency commands also work without the web interface: `phx admin reset-password --email …`,
`phx admin reset-2fa --email …` (removes two-factor and passkeys), `phx admin bot-protection-off` (switches the bot check off everywhere), `phx admin create-token --email … --name …`.

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
and relations in the knowledge graph; boards, floor plans, checks, readings, agent actions and more.

**Phoedox skill**: [`resources/skills/phoedox`](resources/skills/phoedox/SKILL.md) teaches AI clients which tool to use
when and the rules for writing. The MCP server delivers it with the connector (MCP skills extension:
`skills/list`, `skills/get`, resources `skill://phoedox/…`) to clients that support it. Every instance also serves it
at `/api/v1/phoedox-skill.zip` for clients without that support (upload it as a skill); for Claude Code:

```bash
curl -sL https://docs.example.org/api/v1/phoedox-skill.zip -o /tmp/phoedox-skill.zip && unzip -o /tmp/phoedox-skill.zip -d ~/.claude/skills
```

Pages marked as **AI skills** in a project (ADR-0049) are found by AI clients with `list_skills` and `get_skill`.

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

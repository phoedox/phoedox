<p align="center"><img src="docs/logo.svg" width="96" alt="Phoedox-Logo"></p>

# Phoedox

Dokumentation, die weiß, wie alles zusammenhängt. Phoedox verbindet Texte, Grundrisse,
Serverschränke und Home Assistant zu einem Wissensnetz – steuerbar über die Web-App,
die Flutter-App und jede KI per MCP.

> Status: frühes Gerüst. Planung und Entscheidungen: [`docs/decisions.md`](docs/decisions.md).

## Schnellstart

```bash
docker compose up --build
```

Ohne Docker:

```bash
make build && ./bin/phoedox
```

Standard ist SQLite unter `./data`. Für PostgreSQL `PHOEDOX_DB_DRIVER=postgres` und `PHOEDOX_DB_URL` setzen.

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

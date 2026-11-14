# Decisions

**Since 13 Nov 2026, all product and technical decisions are kept in Phoedox itself:**
<https://app.phoedox.com/p/phoedox/decisions> (project `phoedox`, category "Entscheidungen" (decisions)).
They become publicly readable once they are published under `docs.phoedox.com`.

This file is no longer maintained. Its last state is in the Git history (the commit before this
change); the reasons are in ADR-0026.

Creating new decisions: in the interface or via MCP with `create_decision`
(set `supersedes` when replacing an older one); read them with `list_decisions` and `get_document`.

## Transferred decisions

| No. | Title | former section here |
|---|---|---|
| ADR-0001 | Knowledge graph as the core | Product, doc types |
| ADR-0002 | Tech stack | Technology |
| ADR-0003 | Plugins and store: principles | Plugins & store |
| ADR-0004 | Agent for servers, VMs and LXC | Agent, round 5 (agent protocol) |
| ADR-0005 | Connecting Home Assistant | Home Assistant |
| ADR-0006 | Publications and custom domains | Publications & domains, round 5 (publications) |
| ADR-0007 | Architecture: queries, real time, permissions, jobs, distribution | Round 5 |
| ADR-0008 | Operations: image, proxy, cookies | Operations (first version) |
| ADR-0009 | Numbers and superseding for decisions | Decisions (ADRs) |
| ADR-0010 | Accounts, invitations and passwords | Accounts |
| ADR-0011 | Two-factor and passkeys | Two-factor & passkeys |
| ADR-0012 | SSO and OAuth for MCP | SSO & OAuth for MCP |
| ADR-0013 | Search in the database | Search |
| ADR-0014 | History, trash and favorites | History, trash, favorites |
| ADR-0015 | Live editing with Yjs and Lexical | Live editing |
| ADR-0016 | Request blocks and project environment | Request blocks & environment |
| ADR-0017 | Fine-grained control of AI access | AI access |
| ADR-0018 | Data model and knowledge graph | Data model & knowledge graph |
| ADR-0019 | Files: storage, uploads, previews | Files |
| ADR-0020 | Organizations, groups and sharing | Organizations & sharing |
| ADR-0021 | Tenants with Row-Level Security | Tenants (hosted) |
| ADR-0022 | Kubernetes on one server with k3s | Operations: Kubernetes |
| ADR-0023 | Operations: jobs, backups, metrics, API, webhooks | Operations: jobs, backups, … |
| ADR-0024 | Plugin runtime, packages and store | Plugins: runtime, packages, store |
| ADR-0025 | Domains of phoedox.com | Domains of phoedox.com |
| ADR-0026 | Phoedox as the leading source for decisions | – |

Phases, the feature lists of rounds 4 and 5 and the implementation status are on the page
"Fahrplan und Stand" (roadmap and status) in the same project.

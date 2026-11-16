# Knowledge graph

Entries (tools say *entity*) are things like servers, routers, rooms, people or
services: a kind, a name, a slug and attributes. Relations connect them
(`depends_on`, `runs_on`, `located_in`, `uplink`, `wired_to`, `powered_by` …).
Pages mention entries with `@slug`.

## Data model

- `get_data_model` first: the kinds of the project, their attributes as JSON schema
  and which relations are allowed between which kinds. `related_to` and `depends_on`
  work between any kinds.
- If no kind fits, propose one with `define_kind` instead of misusing another kind.
  `define_kind` replaces the project's previous extension of that kind – repeat the
  attributes you want to keep. People see the data model under project settings →
  "Kinds & fields".

## Create and change entries

- `search` or `list_entities` first – never create a second entry for the same thing.
- `upsert_entity` for one, `upsert_entities` for up to 200 in one call (e.g. a whole
  network; failed rows come back in `errors`, the others are saved).
- `link_entities` (one relation per call), `unlink_entities`, `delete_entity` (to the
  trash).
- `list_duplicates` → `merge_entities` (only where AI writes directly; otherwise ask
  a person).

## Read and ask

| Need | Tools |
|---|---|
| A question about a device or service | `search` → `get_entity` (its `context` is a ready summary: values, status, network path, relations, page snippets, what runs on it) |
| What changed on a device, when it last failed | `entity_timeline` (changed fields, check failures and recoveries, switching actions, agent jobs, mentioning comments and cards; newest first) |
| Something does not work | `diagnose` (network path with status per station), `traverse` (e.g. `uplink` to the internet, `depends_on`), `software_tree`, `get_readings` |
| Lists over entries | `query_entities` (field and relation conditions), `list_saved_queries`, `run_saved_query`, `list_entities` |
| Locations, household, network | `list_locations`, `household_overview`, `ip_addresses` |

## Devices from the network

- `list_discovered` lists what agents with discovery on found: IP, MAC, host name,
  mDNS services, suggested kind, matching entry. Create entries with
  `upsert_entities` only for the ones people want documented.
- Switches and routers without an agent come in over SNMP when an agent lists them
  under `snmp.targets` in its agent.yaml (read only): entries of kind switch/router,
  `wired_to` relations with the port from LLDP and the MAC table. Suggest that setup
  instead of typing ports by hand.

## Common relations

- `uplink` towards the internet (device → switch → router).
- `wired_to` a cable between two devices (with ports).
- `located_in` device → room → floor → building → site.
- `runs_on` service → server or VM; `depends_on` service → database, API …
- `powered_by` device → UPS or socket.

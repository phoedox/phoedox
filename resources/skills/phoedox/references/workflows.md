# Workflows

## Document a network or a home

1. `get_project`, `get_data_model` – which kinds exist (router, switch, access_point,
   server, device, room, floor, building, site, subnet …).
2. `search` for each device first. Then `upsert_entity` with useful attributes
   (ip, mac, model, location fields) – one call per entry.
3. Relations: `link_entities` with `uplink` (towards the internet), `wired_to`,
   `located_in` (device → room → floor → building → site), `powered_by`,
   `runs_on` (service → server/VM).
4. A page per area ("Network", "Emergency folder") that mentions the entries with
   `@slug`; tables for overviews. Add a ```status block for live tiles.
5. `diagnose` on a device checks that the path to the internet is complete.

## Onboard a server or service

1. `get_entity` if it exists (the agent may have created it with hardware facts).
2. `software_tree` shows what runs on it.
3. Page "<name>" in the right category: purpose, owner, how to restart, backups,
   dependencies (`@slugs`). Request blocks for health endpoints, then `create_check`
   to monitor one.
4. `link_entities` service `runs_on` server, service `depends_on` database …

## Troubleshoot

1. `search` → `get_entity` for the device the user names.
2. `diagnose` (path and status per station), `get_readings` (history of values,
   e.g. when a switch went off or a temperature dropped), `list_checks`.
3. `traverse` with `depends_on` to see what else is affected.
4. Agent hosts: `list_agent_actions` shows recent jobs and results; propose a
   restart with `prepare_agent_action` – a person signs it.
5. Write what you found into the device's page or a comment (`add_comment`), not
   only into the chat.

## Record a decision

1. `list_decisions` – is it already decided, or does a new one replace an old one?
2. `create_decision` with context, decision, consequences and alternatives, in the
   main language. Set `supersedes` to the replaced number (e.g. `ADR-0007`); do not
   change the old one's status yourself.

## Plan work on a board

1. `list_tasks` (optionally `board`, `column`, `assignee`) to avoid duplicates.
2. `create_task` with a clear title, Markdown description, labels, priority, due date.
3. `update_task` to move cards; moving to the done column completes them.
   `my_tasks` lists what is assigned to the token owner.

## Floor plans

All values in millimetres. Create the room and device entries first. Read the plan
with `get_document` when changing it and keep elements you do not change exactly
(including ids). Rooms are polygons pointing to room entries; devices are points;
walls are segments with a thickness; openings (doors, windows) sit on a wall; cables
are point routes with a kind and optional from/to entries. `save_floorplan` sends the
whole plan; Phoedox links devices `located_in` their room.

## Keep documentation healthy

- `find_documentation_gaps`: failing checks, broken mentions, devices without a
  location, undocumented entries, incomplete entries, empty and stale pages.
- `list_duplicates` → `merge_entities` (only where AI writes directly; otherwise ask
  a person).
- `change_summary` for "what changed this week".
- `archive_link` keeps a copy of a manual a link page points to.
- `save_maintenance_plan` for recurring work (filters, batteries, certificates).

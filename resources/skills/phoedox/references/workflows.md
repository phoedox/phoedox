# Workflows

## Document a network or a home

1. `get_project`, `get_data_model` – which kinds exist (router, switch, access_point,
   server, device, room, floor, building, site, subnet …).
2. `search` (or `list_entities`) for existing devices first. Then create or update
   them with useful attributes (ip, mac, model, location fields): `upsert_entities`
   for many at once (up to 200 per call; failed rows come back in `errors`, the
   others are saved), `upsert_entity` for single ones.
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

## Prepare a decision for people

When a person has to choose (the user asks "what are my options?", or you are not
allowed to decide), do not pick one yourself:

1. `create_decision` with `status: proposed`, the context, and `options`: each
   option has a `title` and a Markdown `description` with pros, cons and cost. The
   `decision` field stays empty; Phoedox assigns option ids (`o1`, `o2`, …).
2. Tell the user to decide on the page: they pick an option (the decision becomes
   `accepted`, the option its decision text, the others its alternatives) or answer
   "nothing fits", each with an optional comment. You never decide.
3. Later, `list_decisions`: `needsOptions: true` means the last answer was "nothing
   fits". Read the `responses` with `get_document` (the comments say what is
   missing) and offer new options with `update_decision` (`options` replaces the
   list; the responses stay).

## Plan work on a board

1. `list_tasks` (optionally `board`, `column`, `assignee`) to avoid duplicates.
2. `create_task` with a clear title, Markdown description, labels, priority, due date,
   and `documents` for the pages and decisions the card belongs to (e.g.
   `["auth-concept", "ADR-0012"]`); `update_task` with `documents` replaces the list.
3. `update_task` to move cards; moving to the done column completes them.
   `my_tasks` lists what is assigned to the token owner.
4. When a card is about code and the project has repositories (`list_code_repos`):
   after the work is pushed, find the commit with `search_commits` and link it with
   `link_task_code` (`kind: commit`, the SHA), or link the pull request
   (`kind: pull`, its number). Do not link releases by hand: Phoedox adds the first
   release that contains a linked commit. `list_tasks` shows the links as
   `codeLinks`. Connecting a repository is for project owners in the settings.

## Whiteboards

A whiteboard is a canvas people draw on together; changes are published at once and
everyone on the board sees them live – also yours. Coordinates are board units (about
pixels at 100 %), y grows downwards.

- **Read first:** `get_whiteboard` returns each element with `id`, `type`, `text`,
  `box` [x, y, w, h] or `points`, bound ends `from`/`to`, frames with their content,
  embeds with title and status, and the open comments with the element they point to.
- **Diagrams:** use `draw_diagram`. Send structure, not coordinates: `nodes` (`id`,
  `text`, `shape` rect|ellipse|diamond|sticky, `color`, `entity` slug, `group`),
  `edges` (`from`, `to`, `label`, `dashed`, `line`), `groups` (`id`, `label` – drawn as
  frames), `direction` right|down, `title`. Phoedox places it right of what is on the
  board and binds the arrows. Without `document` it creates a new whiteboard.
- **Single changes:** `edit_whiteboard` with `ops`: `{op: add, element}`,
  `{op: update, id, element: {changed fields}}` (null removes a field),
  `{op: delete, id}` (arrows bound to it are unbound). An arrow's ends bind with
  `start`/`end`: `{element}` (towards the centre) or `{element, fx, fy}` (a fixed point,
  0–1 of the box); `points` are the waypoints, `round: true` draws smooth corners.
  Embeds take a `url` (video, pull request, issue, website); kind and preview are
  filled in. `entity` on a shape shows that entry; `doc` links a page.
- **Comments:** work through open comments (text or handwritten) and answer each with
  `reply_comment`; people resolve them.

```json
{"project": "home", "title": "Backup flow", "direction": "right",
 "nodes": [{"id": "nas", "text": "NAS", "entity": "nas"},
           {"id": "box", "text": "Storage box (EU)", "color": "green"},
           {"id": "laptops", "text": "Laptops", "shape": "ellipse", "color": "purple"}],
 "edges": [{"from": "laptops", "to": "nas", "label": "Time Machine"},
           {"from": "nas", "to": "box", "label": "nightly 2:00", "dashed": true}]}
```

## Floor plans

All values in millimetres. Create the room and device entries first. Read the plan
with `get_document` when changing it and keep elements you do not change exactly
(including ids). Rooms are polygons pointing to room entries; devices are points;
walls are segments with a thickness; openings (doors, windows) sit on a wall; cables
are point routes with a kind and optional from/to entries. `save_floorplan` sends the
whole plan; Phoedox links devices `located_in` their room.

A complete plan of a 6 × 4 m flat with two rooms, one wall with a door and a router.
Ids are your own short names (letters, digits, `-`, `_`); room points run clockwise;
`entity` fields are slugs of existing entries. Pass `document` (the slug) instead of
`title` to replace an existing plan.

```json
{
  "project": "home",
  "title": "Ground floor",
  "floorplan": {
    "floor": "ground-floor",
    "width": 6000,
    "height": 4000,
    "rooms": [
      { "id": "r1", "name": "Living room", "entity": "living-room", "color": "blue",
        "points": [[0, 0], [4000, 0], [4000, 4000], [0, 4000]] },
      { "id": "r2", "name": "Office", "entity": "office", "color": "green",
        "points": [[4000, 0], [6000, 0], [6000, 4000], [4000, 4000]] }
    ],
    "walls": [
      { "id": "w1", "a": [4000, 0], "b": [4000, 4000], "thickness": 115 }
    ],
    "openings": [
      { "id": "o1", "wall": "w1", "kind": "door", "offset": 2000, "width": 900 }
    ],
    "devices": [
      { "id": "d1", "entity": "router", "x": 4800, "y": 600 }
    ],
    "cables": [
      { "id": "c1", "kind": "network", "points": [[4800, 600, 300], [1000, 600, 300]],
        "from": "router", "to": "tv" }
    ]
  }
}
```

Colors: gray, red, orange, yellow, green, blue, purple, pink. Cable points are
`[x, y, height above the floor]`; kinds are network, power, coax and other.

## Move existing documentation into Phoedox

1. Read the whole source first and draft the target structure before writing
   anything: top-level categories (e.g. Architecture, Guides, Operations, Legal,
   Plans, Archive) and, where a category would get more than about eight pages,
   sub-folders by topic with `/` (`Architecture/Backend`, `Architecture/Apps`,
   `Architecture/Data & sync`, `Architecture/AI & integrations`).
2. Show the user the structure (categories, folders, page titles) and wait for an
   answer before creating pages – moving many pages later is tedious.
3. Strip prefixes that only repeat the folder: "Architecture: auth concept" in
   `Architecture` becomes "Auth concept"; "Moderation – plan" in `Plans`
   becomes "Moderation". Keep words that carry meaning ("API overview").
4. Put plans and ideas into `Plans`, outdated or replaced pages into `Archive`
   (or mark them as superseded decisions), not between current pages.
5. Decisions go through `create_decision` (keep old numbers in the title, e.g.
   "D-321: …", so `[[D-321]]` keeps working), tasks onto a board, devices and
   services into entries – not as plain pages.
6. Rewrite internal links to `[[slug]]`, keep external links, and upload images
   with `upload_image` instead of linking to the old system.
7. Finish with `get_project` and check the sidebar: no folder with a single page,
   no category that differs only in spelling, no title starting with its folder name.

## Keep documentation healthy

- `find_documentation_gaps`: failing checks, broken mentions, devices without a
  location, undocumented entries, incomplete entries, empty and stale pages.
- `list_duplicates` → `merge_entities` (only where AI writes directly; otherwise ask
  a person).
- `change_summary` for "what changed this week".
- `archive_link` keeps a copy of a manual a link page points to.
- `save_maintenance_plan` for recurring work (filters, batteries, certificates).

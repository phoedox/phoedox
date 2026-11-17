# Workflows

Flows that use several areas. The tools of each area are explained in its own
reference file (pages, decisions, knowledge graph, tasks, operations).

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
5. Decisions go through `create_decisions` (up to 50 per call) with their old
   number as `ext_ref` (e.g. "D-321"), so `[[D-321]]` and `get_document("D-321")`
   keep working; tasks go onto a board, devices and services into entries – not as
   plain pages.
6. Rewrite internal links to `[[slug]]`, keep external links, and upload images
   with `upload_image` instead of linking to the old system.
7. When the structure has to change later, move pages with `batch_update`
   (`category`, `after`) – moving creates no new versions.
8. Finish with `get_project` and check the sidebar: no folder with a single page,
   no category that differs only in spelling, no title starting with its folder name.

## Phoedox instead of docs/ in a code repository

1. Move the Markdown files in as above; keep files that are code (an OpenAPI file
   the server embeds, brand assets for build scripts) in the repository.
2. In the repository's `CLAUDE.md` (or `AGENTS.md`), name the Phoedox project and the
   pages to read before changes, and let coding sessions read them over MCP:
   `get_document` with `heading` for one section, `search` for the rest, decisions
   by their own number (`get_document("D-649")`).
3. Replace links in code and texts (`--help`, READMEs, error messages) with the
   Phoedox addresses (`url` from the tool results). Pages that must be public
   (DEPLOY, CONFIG, legal) go into a publication that lists only those pages;
   the publication's fixed links (by page id) keep working after renames.
   People set that up in Phoedox (Publications).
4. Record new decisions with `create_decision` in the same session as the code
   change, and link the commit or pull request to the card (`link_task_code`).
5. Docs that change with a pull request: pass `pull_request` (`owner/repo#12`,
   `#12`, the pull request URL, or `owner/repo@branch` before the pull request
   exists) to `update_document` or `update_decision`. The change goes into a
   draft tied to the pull request; the pull request gets a "Docs changes" comment
   with links; merging publishes the drafts, closing without merge discards them.
   The published page stays unchanged until then, so other sessions keep reading
   the current state. New pages are created right away – mention them in the
   pull request instead.
6. Review docs per pull request with `change_summary` and `pull_request`: each
   changed page or decision with a line diff.
7. Leaving or backing up: `export_project` gives the owner a 15-minute download
   link – a ZIP with Markdown (front matter) plus JSON and files that Phoedox can
   import again, or with `format: git` a Git repository with one commit per
   version.

## Record what you learned

- A decision becomes a decision (`create_decision`, see `decisions.md`), work to do a
  card on a board (`tasks.md`), facts about devices into entries (`knowledge-graph.md`), and
  explanations into pages (`pages.md`) – not all into one page.
- Keep the documentation healthy afterwards: `find_documentation_gaps`,
  `list_duplicates`, `change_summary` (see `operations.md`).


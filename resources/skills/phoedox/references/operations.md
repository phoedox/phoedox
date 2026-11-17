# Operations: monitoring, APIs, maintenance, real devices

## Monitoring and APIs

- `list_environment` lists the project's `{{variables}}` (secret values are never
  shown). Request blocks on pages use them (see `page-syntax.md`).
- `run_request` runs a request block of a page – only GET, HEAD and OPTIONS. People
  run any method from the page.
- `create_check` monitors a reading request block on a schedule; `list_checks` shows
  checks with their last state.
- `get_readings` returns the history of measured values (e.g. when a switch went off
  or a temperature dropped).

## Maintenance

`list_maintenance_plans`, `save_maintenance_plan` for recurring work (filters,
batteries, certificates) with an interval and the entries it applies to.

## Real devices

- Agent hosts: `list_agent_actions` shows recent jobs and results; propose an action
  (restart a service, update packages …) with `prepare_agent_action` – a person signs
  it in the browser, the agent checks the signature.
- Plugin tools `plugin_*` (e.g. Home Assistant) read and switch devices. Switching
  becomes a suggestion unless an owner allowed AI control for that one device.
- Never retry an action that became a suggestion; tell the user.

## Keep documentation healthy

- `find_documentation_gaps`: failing checks, rule violations, pages and entries whose
  review date passed (`review_due`), broken mentions, devices without a location,
  undocumented and incomplete entries, empty and stale pages.
- Review dates: `get_document` shows `review` (interval, last review, `due`,
  `overdue`). `set_page_review` sets the interval (`everyDays`) and required
  reading; with `reviewed: true` it marks the page as checked – only after you
  verified that it is still correct (e.g. while working through `review_due`).
- `list_duplicates` → `merge_entities` (only where AI writes directly).
- `change_summary` for "what changed this week"; with `pull_request` the doc
  changes waiting for that pull request (see `workflows.md`).
- `export_project` (owners): a download link to the whole project as ZIP or Git.
- `archive_link` keeps a copy of a manual a link page points to.

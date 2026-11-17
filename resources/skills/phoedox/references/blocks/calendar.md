# ```calendar – the project's dates

Shows the project's dates as a month or a list (ADR-0089): maintenance plans (with the
following dates), cards with a due date, date fields of entries (e.g. a contract's
`cancel_by`, a car's `next_inspection`) and events of external calendars the project
connected (CalDAV or iCal, set up by people in project settings → Calendars).

One `key: value` per line, all optional:

| Key | Meaning |
|---|---|
| `view` | `month` (default) or `list` |
| `sources` | comma list of `maintenance`, `tasks`, `entities`, `external` (default: all) |
| `kinds` | only entries of these kinds (date fields and maintenance) |
| `calendar` | a calendar document's slug: use its filters |
| `days` | list view: how many days ahead (default 60) |

````markdown
```calendar
view: list
sources: maintenance, entities
kinds: car, contract
days: 90
```
````

- A whole calendar page is a document of type `calendar`: create it with `create_calendar`
  (title, category, `calendar: {view, sources, kinds, calendars, days}`).
- Read the dates yourself with the tool `calendar` (from, to, sources, kinds, or a calendar
  document) – e.g. to answer "what is due next month?". Each event has `start`/`end`
  (all-day dates or times, end exclusive), `source`, `link` (`e/<entry>` or
  `d/<board>?task=<id>`) and, for date fields, `attr`.
- To put a date into the calendar, set it where it belongs: a date field of the entry
  (`upsert_entity`), a card's `due` (`update_task`) or a maintenance plan
  (`save_maintenance_plan`). Phoedox writes these into external CalDAV calendars that
  have "write back" switched on; external events themselves are read only.
- `list_external_calendars` shows the connected calendars (no passwords). Adding one
  needs a password, so people do it in Phoedox.

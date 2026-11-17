# Page syntax

Pages are CommonMark with GitHub tables, task lists and these additions.

## Mentions and links

`@slug` links an entry of the same project, `@project/slug` one of another project.
Mentions inside inline code or code blocks stay text. `@` does not work for pages:
link a page or decision with `[[ref]]` or `[[ref|label]]`, where ref is its slug,
its number (`[[ADR-0255]]`) or the start of its title up to a separator
(`[[D-321]]` finds "D-321 · iOS project …"); `[[other-project/ref]]` links across
projects. A ref without a project always means the project of the page or card it
is written in – `[[ADR-0001]]` in project A never reaches ADR-0001 of project B, so
write `[[b/ADR-0001]]` there. Plain Markdown links `[Title](/p/<project>/d/<slug>)` work too.
Published sites rewrite both to their own addresses.

**Auto-links:** plain references become links when a page is shown, without
changing the text: decision numbers (`ADR-0012`), documents' own numbers (ext refs
such as `D-640`) and section numbers (`§6.4` → the heading "6.4 …" of the same
page). Projects add own rules (e.g. `JIRA-\d+` → the issue tracker) with
`update_project_settings` `auto_links`. So you need no `[[…]]` for these; use it
for links with another label or to other projects.

## Callouts

```markdown
> [!note]
> Text of the callout.
```

Kinds: `note`, `tip`, `important`, `warning`, `caution`, `faq`. Case does not matter (`[!NOTE]` as on
GitHub works too); Phoedox saves them lower case.

- **Title:** text after the marker is the callout's title: `> [!tip] Before you start`.
- **Foldable** (as in Obsidian): `-` after the bracket folds the callout closed, `+` shows it
  open; readers click the title. Use it for FAQs, long logs and background that most readers
  skip. Print and PDF show the content.

```markdown
> [!faq]- Why does the NAS go to sleep at night?
> Its disks spin down after 20 minutes without access.
```

- **Tabs:** `> [!tab] Title` callouts that follow each other (blank lines between are fine)
  become one group of tabs – for variants of the same thing (Windows / macOS / Linux,
  tenants / owners). A reader's choice also selects the tab with the same title in other
  groups of the page.

```markdown
> [!tab] Windows
> Press **Win + R** and type `cmd`.

> [!tab] macOS
> Open **Terminal** from Launchpad.
```

- **Steps:** `> [!steps id=<short id>] Title` with a numbered list inside: each reader ticks
  the steps off for themselves and sees their progress. Give it an `id` (letters, digits,
  `-`) so ticks survive edits of the text.

```markdown
> [!steps id=router-setup] Set up the new router
> 1. Connect the fibre modem to port WAN.
> 2. Open `http://192.168.1.1` and sign in.
> 3. Set the Wi-Fi name and password from the label.
```

**Comparing two versions** of a file or setting: a ```compare block; `--- title` starts the
old version, `+++ title` the new one. Differences are shown side by side.

````markdown
```compare
--- router.conf (before)
address 192.168.1.1/24
+++ router.conf (after)
address 192.168.10.1/24
mtu 9000
```
````

## Request blocks

````markdown
```request
GET {{base_url}}/api/status
Authorization: Bearer {{token}}

expect status == 200
expect $.state == "ok"
```
````

Each `expect` line is `<subject> <operator> <value>`:

| Subject | Means |
|---|---|
| `status` | HTTP status code |
| `time` | response time in ms |
| `header <Name>` | a response header, e.g. `header Content-Type contains json` |
| `$.a.b[0].c` | a JSON path into the body: `.field`, `[index]`, and `.length` at the end for arrays, strings and objects |

Operators: `==`, `!=`, `<`, `<=`, `>`, `>=` (numbers are compared as numbers),
`contains` (substring, or an array element), `exists`, `!exists` (no value).
Values are numbers, `"strings"`, `true`, `false`, `null` or bare text. Examples:
`expect time < 500`, `expect $.items.length >= 1`, `expect $.version exists`.

`{{variables}}` come from the project environment (`list_environment`; secret values
are never shown). People run any method from the page; `run_request` runs only GET,
HEAD and OPTIONS. `create_check` monitors a reading block on a schedule.

## Live blocks

Each is a fenced code block with this info string:

| Block | Content | Shows |
|---|---|---|
| ```status | one `@entry` per line, `checks` for all checks, `check: Name` for one | live state tiles |
| ```entity | an entry slug | a card with attributes and relations |
| ```include | a page slug | the other page's content, always current |
| ```query | a saved query slug | the query result as a table |
| ```board | a board slug | the board's cards |
| ```whiteboard | a whiteboard slug | the whiteboard drawn read-only, with a link to open it |
| ```app | an app slug | the app running inside the page, as tall as its content (each reader allows its access once) |
| ```map | lines: `marker 52.52, 13.40 Label`, `marker @entry` (uses its lat/lng), `track Name: lat,lng lat,lng …`, `gpx <file id> Name`, `view lat,lng zoom 12` | an OpenStreetMap map (tiles after consent) |
| ```mermaid | a Mermaid diagram | the diagram |
| ```compare | two versions: `--- old title` … `+++ new title` … | the versions side by side, differences marked |
| ```chart | chart JSON (see below) | a line, area, bar or scatter chart; annotated timelines with numbered points, phases and a list |
| ```poll | `question:` line, `- option` lines; `multiple`, `closed: yes`, `results: always`, `id:` | a poll: each signed-in reader votes (or changes their vote); results as bars after voting; counts only, never who voted |
| ```matrix | `criteria: Price*3, Noise*2`, `scale: 1-5`, `- Option: 4, 3` | a decision matrix with weighted totals; readers try other weights; one click makes it an open decision with the options |
| ```calc | `Label (unit) = 25` (input), `Label (unit) = formula`, `Label = @entry.attr` | a calculator: readers change inputs and see results at once |
| ```quiz | `? question`, `+ right` / `- wrong` answers or `= answer` (flashcard), `> explanation` | a quiz or flashcards; each reader's answers are their own |
| ```mindmap | an indented outline (first line = centre) | a mind map; readers collapse branches and copy it onto a whiteboard |
| ```gantt | `Name [id]: start, end or duration`, `## Section`, `today`, `board: slug` | a schedule with sections, milestones, progress and dependency arrows |
| ```beforeafter | `before: <image> label`, `after: <image> label` | two photos with a slider between them |
| ```annotate | `image: <image>`, `- x, y: text` per marker | a photo with numbered markers and their list |
| ```tree | `root: @entry`, `relation: wired_to`, `direction: in` or `out`, `depth: 4` | a tree of entries along one relation (network, team, family) with live status |
| ```rack | the rack's entry, e.g. `@rack-basement` | the rack by height unit with the mounted devices and their live status |
| ```floorplan | `plan: <floor plan>`, optional `room: <room>`, `devices: no` | a floor plan or one room of it with its devices, always current |
| ```math | LaTeX | a formula (inline: `$…$`) |
| ```dbmodel | SQL `CREATE TABLE` statements | an entity-relationship diagram |
| ```embed | a URL | an embedded video or page (e.g. YouTube) |

Mermaid ids must not be Mermaid keywords: in a `sequenceDiagram`, a participant called
`Off`, `End`, `Loop`, `Alt` or `Note` breaks the diagram (`participant Remote as Off-site`
works). Pages show such an error in the block. `validate_blocks` checks mermaid,
dbmodel, chart, poll, matrix, calc, quiz, mindmap, gantt, beforeafter, annotate, tree, rack and floorplan blocks before or after saving (common mistakes, not the full Mermaid parser);
`create_document` and `update_document` return the same findings as `blockProblems`.

**One database schema per project.** All `dbmodel` blocks of a project together form its
schema. A block draws its own `CREATE TABLE`s; a reference to a table defined on another
page is drawn too (reduced to its keys, with a link to that page). To show tables defined
elsewhere, list them in an SQL comment – no placeholder tables needed:

```dbmodel
-- tables: users, sessions
CREATE TABLE devices (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));
```

**From a repository:** a block whose first line is `from owner/repo path` shows the
schema that the SQL migrations in that folder of a connected repository build
(project settings → Code & Git), in file order with `ALTER TABLE` and `DROP TABLE`
applied and down parts (goose, dbmate, `*.down.sql`) left out. It stays current
with the repository, so prefer it to copying `CREATE TABLE`s when the code lives
in a connected repository:

```dbmodel
from acme/app db/migrations/postgres
```

### Charts

A ```chart block holds one JSON object. Use it for numbers over time or per thing,
and for **annotated timelines**: a curve with numbered events, phases behind it and
the events listed with their text under the chart (hovering a point shows it too).

| Field | Meaning |
|---|---|
| `type` | `line` (default), `area`, `bar`, `scatter` |
| `title`, `description`, `height` | heading, a sentence under it, plot height in px (160–800, default 300) |
| `points` or `series` | one series as `points`, or `series: [{name, color, curve: smooth\|straight\|step, dashed, points}]` |
| point | `{x, y, label, title, text, color}` – `x` a date (`1848`, `1848-03`, `1848-03-18`), a number or a name (bars); `y` a number or `null` (a gap); `label` is drawn in the marker (e.g. its number); points with `title`/`text` are listed under the chart |
| `x` | `{label, type: time\|number\|category, segments: [{from, to, weight, label}]}` – segments sit side by side, each with its own scale: compress quiet decades and stretch busy years (`weight` = share of the width); a dotted line marks the border |
| `y` | `{label, min, max, unit, ticks}` – `ticks` may carry words: `[{value: 0, label: "low"}, {value: 10, label: "high"}]` for qualitative scales |
| `bands` | phases behind the plot: `[{from, to, label, color}]`; points without a colour take their phase's colour, and the legend lists the phases |
| `notes` | short texts on the plot: `[{x, y, text, color}]` |
| `list` | `false` hides the numbered list |

Colours are the board palette: gray, red, orange, yellow, green, blue, purple, pink
(they follow light and dark mode). A dashed second series continues a curve
("ideas live on"). `validate_blocks` checks charts too.

```chart
{"title": "Pressure on the old powers",
 "x": {"segments": [{"from": "1815", "to": "1847-12", "label": "compressed"},
                    {"from": "1848-01", "to": "1849-10", "weight": 1.2, "label": "stretched"}]},
 "y": {"ticks": [{"value": 0, "label": "low"}, {"value": 5, "label": "medium"}, {"value": 10, "label": "high"}]},
 "bands": [{"from": "1815", "to": "1848-02", "label": "Restoration", "color": "orange"},
           {"from": "1848-02", "to": "1848-05", "label": "March revolution", "color": "red"},
           {"from": "1848-05", "to": "1849-10", "label": "Counter-revolution", "color": "gray"}],
 "series": [{"points": [
   {"x": "1815", "y": 1, "label": "1", "title": "1815 – Congress of Vienna", "text": "Restoration."},
   {"x": "1832", "y": 4, "label": "2", "title": "1832 – Hambach Festival"},
   {"x": "1848-03-18", "y": 9.5, "label": "3", "title": "March 1848", "text": "Barricades in Berlin."},
   {"x": "1849-07", "y": 1, "label": "4", "title": "July 1849 – the revolution ends"}]},
   {"dashed": true, "points": [{"x": "1849-07", "y": 1}, {"x": "1849-10", "y": 2.5}]}],
 "notes": [{"x": "1849-09", "y": 3, "text": "ideas live on"}]}
```

### Interactive blocks

**Polls** (```poll): the first plain line or `question:` is the question, `- …` lines
are the options (2–20, no duplicates). `multiple` allows several answers (percentages
are then of the people who voted), `closed: yes` ends voting, `results: always` shows
results before voting. Give `id: …` when the poll may be moved or its text edited, so
votes stay attached. Votes are stored per person; nobody sees who voted what.

**Decision matrices** (```matrix): `criteria:` lists criteria with an optional weight
(`Price*3`), `scale: 1-5` the best score, `- Option: score, score, …` one score per
criterion, higher is better (score cost so that cheaper is higher). The total is the
weighted sum as a share of the best possible. Use it to prepare a decision; readers can
turn it into an open decision (ADR with the options) themselves.

**Calculators** (```calc): one line per value, `Label (unit) = …`. A number is an input
readers can change, `@entry.attr` takes an entry's attribute as input, anything else is a
formula. Formulas use other values by their label with spaces as `_` and without the unit
(`Hours per day (h)` → `Hours_per_day`), `+ - * / ^ %`, comparisons, `&` (joins text) and
SUM, AVG, MIN, MAX, ROUND(x; digits), CEIL, FLOOR, ABS, SQRT, IF(cond; a; b). Decimal
comma or point both work. Nothing is saved.

**Quizzes** (```quiz): `? question`, then `+ right` and `- wrong` answers (several `+`
allowed) or `= answer` for a flashcard, optionally `> explanation`. Good for onboarding
("Do you know our network?"). `validate_blocks` checks all four.

```calc
title: Power cost of the NAS
Power (W) = 25
Price (€/kWh) = 0.32
Cost per year (€) = Power * 24 * 365 / 1000 * Price
```

### Blocks from the knowledge graph

They only name entries and documents and always show the current state;
`validate_blocks` warns when a named entry, floor plan or room does not exist.
Published pages leave trees and racks out (entries stay private) and draw floor plans
that are part of the publication.

**Trees** (```tree): `root: @entry` and the `relation` to follow. `direction: in`
(default) collects entries whose relation points *to* the root – switches and devices
`wired_to` the router (or `uplink` over Wi-Fi), people with `reports_to` their lead; `direction: out` follows the
relation from the root (e.g. `parent_of`). `depth` 1–8 (default 4). Without a relation
every relation is followed.

**Racks** (```rack): the rack entry. Devices appear by their `mounted_in` relation with
`u` (lowest height unit) and `height` on the relation; the rack's `height_units`
attribute sets its size.

**Floor plan excerpts** (```floorplan): `plan:` the floor plan page, `room:` a room by
its name or entry to cut the view to it; devices placed on the plan are shown with
their live status (`devices: no` hides them).

### Pictures and plans

**Mind maps** (```mindmap): the first line is the centre; lines indented under a line
(spaces or `- ` lists, the same step throughout) are its branches. At most 400 items.

**Schedules** (```gantt): one line per item, `Name [id]: start, end or duration, …`.
Start is a date (`2026-03-01`) or `after id` (several ids: `after a b`); then an end date,
a duration (`3d`, `2w`, `1m`) or nothing for a milestone; optionally `done`, `50%` and a
colour. `## Section` groups items, `today` draws today's line, `board: slug` adds the
board's cards that have a due date (from the day they were created to the due date).
A range can also be written `2026-02-02 – 2026-02-06`.

```gantt
title: Bathroom renovation
today
## Planning
Get quotes [quotes]: 2026-01-05, 2w, done
Choose a contractor [choose]: after quotes, 3d
## Work
Demolition [demo]: after choose, 1w
Tiles: after demo, 10d, 30%
Done: 2026-03-20
```

**Images in blocks** (`beforeafter`, `annotate`) are files of the project
(`/api/v1/files/<id>`, see `list_files` and `upload_image`) or https addresses. Attach the
file to the page so published pages show it. Words after the address are the label.

**Before/after** (```beforeafter): `before: <image> label` and `after: <image> label`
(or two Markdown images), optional `title:` and `start: 30` (slider position in percent).

**Annotated images** (```annotate): `image: <image> description`, then one line per
marker: `- x, y: text` with x and y in percent of the picture (0–100, from the top left),
optionally a colour after the position (`- 74, 64 blue: RCD`). `@entry` in a text links
the entry. Markers are numbered in order. People can also place markers by clicking in
the editor. `validate_blocks` checks all four.

## Images and files

Images are files of the project: `![alt](/api/v1/files/<id>)`. `list_files` shows what
exists. To add a picture yourself (screenshot, photo, diagram, logo), send it as base64 with
`upload_image` (PNG, JPEG, GIF, WebP or SVG; SVGs are cleaned – scripts, event handlers and
outside links are removed):

- with `task`: the picture is stored with the card's board and appended to the card's
  description (it becomes the card's cover if it is the first picture);
- with `document` or `entity`: the picture is attached there; for a page, insert the
  returned `markdown` with `update_document`;
- only PNG, JPEG, GIF or WebP up to 8 MB, and only where the project lets AI write directly.

`set_project_image` sets a project's logo from base64 (owners; scaled to 512 px).

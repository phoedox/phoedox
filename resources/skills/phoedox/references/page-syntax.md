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

## Blocks

Blocks are fenced code blocks whose info string names the kind. **Each kind has its own
reference file in `blocks/<kind>.md`** (source format, an example, checks, how it looks on
whiteboards and published pages) – read the one you are about to write.

| Block | Shows | Reference |
|---|---|---|
| ```request | an HTTP request with `expect` checks, run from the page | `blocks/request.md` |
| ```compare | two versions of a file side by side | `blocks/compare.md` |
| ```status | live state tiles of entries and checks | `blocks/status.md` |
| ```entity | an entry as a card | `blocks/entity.md` |
| ```include | another page's content, always current | `blocks/include.md` |
| ```query | a saved query as a table | `blocks/query.md` |
| ```board | a board's cards | `blocks/board.md` |
| ```whiteboard | a whiteboard read-only | `blocks/whiteboard.md` |
| ```app | an app running inside the page | `blocks/app.md` |
| ```map | an OpenStreetMap map with markers and tracks | `blocks/map.md` |
| ```mermaid | a Mermaid diagram | `blocks/mermaid.md` |
| ```math | a LaTeX formula (inline: `$…$`) | `blocks/math.md` |
| ```dbmodel | the project's database schema as a diagram | `blocks/dbmodel.md` |
| ```embed | a video or web page | `blocks/embed.md` |
| ```chart | line, area, bar, scatter charts and annotated timelines | `blocks/chart.md` |
| ```poll | a poll readers vote in | `blocks/poll.md` |
| ```matrix | a weighted decision matrix (plugin `com.phoedox.matrix`) | `blocks/matrix.md` |
| ```calc | a calculator with inputs and formulas (plugin `com.phoedox.calc`) | `blocks/calc.md` |
| ```quiz | a quiz or flashcards | `blocks/quiz.md` |
| ```mindmap | a mind map from an outline (plugin `com.phoedox.mindmap`) | `blocks/mindmap.md` |
| ```beforeafter | two photos with a slider | `blocks/beforeafter.md` |
| ```annotate | a photo with numbered markers | `blocks/annotate.md` |
| ```tree | a tree of entries along one relation | `blocks/tree.md` |
| ```rack | a rack by height unit with its devices | `blocks/rack.md` |
| ```floorplan | a floor plan or one room of it | `blocks/floorplan.md` |

**Blocks from plugins** (e.g. ```gantt schedules from `com.phoedox.gantt`): `get_project`
lists a project's plugin blocks with an example; see `blocks/plugins.md`.

`validate_blocks` checks blocks before or after saving; `create_document` and
`update_document` return the same findings as `blockProblems`. The same source goes on
whiteboards as `{type: block, kind, source}` (`whiteboards.md`) for every kind whose file
says so.

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

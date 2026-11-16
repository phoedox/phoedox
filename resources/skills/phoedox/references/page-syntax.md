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

## Callouts

```markdown
> [!note]
> Text of the callout.
```

Kinds: `note`, `tip`, `important`, `warning`, `caution`.

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
| ```map | lines: `marker 52.52, 13.40 Label`, `marker @entry` (uses its lat/lng), `track Name: lat,lng lat,lng …`, `gpx <file id> Name`, `view lat,lng zoom 12` | an OpenStreetMap map (tiles after consent) |
| ```mermaid | a Mermaid diagram | the diagram |
| ```math | LaTeX | a formula (inline: `$…$`) |
| ```dbmodel | SQL `CREATE TABLE` statements | an entity-relationship diagram |
| ```embed | a URL | an embedded video or page (e.g. YouTube) |

Mermaid ids must not be Mermaid keywords: in a `sequenceDiagram`, a participant called
`Off`, `End`, `Loop`, `Alt` or `Note` breaks the diagram (`participant Remote as Off-site`
works). Pages show such an error in the block. `validate_blocks` checks mermaid and
dbmodel blocks before or after saving (common mistakes, not the full Mermaid parser);
`create_document` and `update_document` return the same findings as `blockProblems`.

**One database schema per project.** All `dbmodel` blocks of a project together form its
schema. A block draws its own `CREATE TABLE`s; a reference to a table defined on another
page is drawn too (reduced to its keys, with a link to that page). To show tables defined
elsewhere, list them in an SQL comment – no placeholder tables needed:

```dbmodel
-- tables: users, sessions
CREATE TABLE devices (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));
```

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

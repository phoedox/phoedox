# Page syntax

Pages are CommonMark with GitHub tables, task lists and these additions.

## Mentions

`@slug` links an entry of the same project, `@project/slug` one of another project.
Mentions inside inline code or code blocks stay text.

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
| ```map | lines: `marker 52.52, 13.40 Label`, `marker @entry` (uses its lat/lng), `track Name: lat,lng lat,lng …`, `gpx <file id> Name`, `view lat,lng zoom 12` | an OpenStreetMap map (tiles after consent) |
| ```mermaid | a Mermaid diagram | the diagram |
| ```math | LaTeX | a formula (inline: `$…$`) |
| ```dbmodel | SQL `CREATE TABLE` statements | an entity-relationship diagram |
| ```embed | a URL | an embedded video or page (e.g. YouTube) |

## Images and files

Images are files of the project: `![alt](/api/v1/files/<id>/<name>)`. Upload happens
in the app; `list_files` shows what exists.

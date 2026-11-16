# Whiteboards

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

## Other tools

- `save_whiteboard` creates or replaces a whole board (e.g. an import); for changes
  use `edit_whiteboard`, for diagrams `draw_diagram`.
- Pages can show a whiteboard read-only with a ```whiteboard block (see
  `page-syntax.md`).

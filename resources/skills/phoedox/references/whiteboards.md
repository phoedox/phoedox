# Whiteboards

A whiteboard is a canvas people draw on together; changes are published at once and
everyone on the board sees them live – also yours. Coordinates are board units (about
pixels at 100 %), y grows downwards.

- **Read first:** `get_whiteboard` returns each element with `id`, `type`, `text`,
  `box` [x, y, w, h] or `points`, bound ends `from`/`to`, frames with their content,
  embeds with title and status, prototypes (`prototypes` with variables and data;
  screens are frames with `screen`, components have `ui` and `actions`) and the open
  comments with the element they point to (comments left while playing point to a
  screen).
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
  filled in. An app of the project goes on the board as `{type: embed, kind: app,
  doc: <app slug>, w, h}`; people start it there and it runs with their rights.
  `entity` on a shape shows that entry; `doc` links a page.
- **Blocks:** `{type: block, kind, source, x, y, w, h}` puts a content block on the
  board; `source` is the same text as the fenced block of that kind in a page
  (`blocks/<kind>.md`), e.g. `kind: chart` with the chart JSON. Blocks look the same as in
  pages; polls and calculators can be used right on the board, decision matrices show
  their bars; trees, racks and floor plan excerpts are live; mind maps, schedules, before/after and annotated images are pictures
  (quizzes are for pages only). A mind map in a page can be copied onto a board as
  shapes; for editable diagrams use `draw_diagram`. About 560×360 fits a
  chart. Older boards may have `type: chart` with a `chart` object – read it as a chart
  block; write new ones as `type: block`.
- **Mermaid:** `draw_diagram` also takes `mermaid` (a flowchart: `graph`/`flowchart`
  with node shapes, labelled, dotted and thick links, `&`, subgraphs); it becomes
  editable shapes and bound arrows. Prefer it when you already have a flowchart.
- **What people see:** entries on a board show their live status (checks, path
  diagnosis); pull requests and issues of the project's code connection show their
  current state; your changes appear with your own cursor. People can turn sticky
  notes into cards and draw an entry's neighbourhood from the graph themselves.
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

## Prototypes

A prototype is a set of screens on a board that people play like an app: full screen in a
device frame, with working buttons, fields, lists and navigation. Use it for app screens,
flows and small tools; for a real program write an app (`create_app`).

- **Draw:** `draw_screens` with `prototype` (a name; an existing prototype is extended and
  a screen with the same name is redrawn in place), `device` phone|tablet|desktop, `look`
  "" or `sketch` (wireframe), and `screens`: `{name, start, overlay modal|sheet, actions,
  elements}`. Elements stack from the top unless `x`/`y` (relative to the screen) are
  given; a `navbar` goes to the bottom.
- **Components** (`comp`): button, link, input, textarea, checkbox, switch, radio, select,
  slider, tabs, navbar, list, card, table, toast, icon, chart, avatar, badge, progress.
  `text` is the label; `variant` primary|secondary|ghost|danger; `placeholder`, `value`,
  `options` (radio, select, tabs, navbar, list), `icon` (a Lucide name), `fields` (list:
  title and subtitle field; table and chart: columns; tabs and navbar: one icon per
  option), `min`/`max`, `show` (`"var"`, `"!var"`, `"var=value"`). Plain elements:
  `{type: text|rect|ellipse|sticky|line, text, fontSize}`.
- **State:** `vars` `[{name, type bool|text|number|list, value}]`; `bind` connects a
  component to a variable; `{{var}}` in any text inserts it (`{{list.length}}` counts),
  `{{row.field}}` a list or table row's field.
- **Data:** `data` `[{name, source, ref, rows}]`: `sample` rows, or live and read-only
  `query` (a saved query), `tasks` (a board's cards) and `checks`. Give live sources
  sample `rows` too: share links and exported files show those.
- **Interactions** (`actions` on any element, or on a screen for `open`/`delay`):
  `{on click|change|hover|open|delay, do goto|back|overlay|close|set|toggle|inc|add|link|toast,
  target (a screen name, or a variable), value, item (only for this option or list row),
  anim slide|fade, delay}`.
- **What people do with it:** play it (▶ on the board), comment on screens while playing
  (the comments are normal board comments – answer them with `reply_comment`), export one
  HTML file, or turn it into an app document to build on.

```json
{"project": "shop", "prototype": "Checkout", "device": "phone",
 "vars": [{"name": "count", "type": "number", "value": 1}],
 "screens": [
  {"name": "Cart", "start": true, "elements": [
    {"type": "text", "text": "Your cart", "fontSize": 28},
    {"comp": "card", "text": "Coffee beans", "value": "{{count}} × 12.90 €"},
    {"comp": "button", "text": "One more", "variant": "secondary", "actions": [{"on": "click", "do": "inc", "target": "count"}]},
    {"comp": "button", "text": "Pay", "actions": [{"on": "click", "do": "overlay", "target": "Paid"}]}]},
  {"name": "Paid", "overlay": "sheet", "elements": [
    {"type": "text", "text": "Thank you!", "fontSize": 24},
    {"comp": "button", "text": "Close", "actions": [{"on": "click", "do": "close"}]}]}]}
```

## Other tools

- `save_whiteboard` creates or replaces a whole board (e.g. an import); for changes
  use `edit_whiteboard`, for diagrams `draw_diagram`.
- Pages can show a whiteboard read-only with a ```whiteboard block (see
  `blocks/whiteboard.md`).

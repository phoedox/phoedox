# Whiteboards

A whiteboard is a canvas people draw on together; changes are published at once and
everyone on the board sees them live – also yours. Coordinates are board units (about
pixels at 100 %), y grows downwards.

People can maximize a whiteboard on its page or open it as a page of its own that
shows only the board: `https://<instance>/wb/<project>/<document id>`. Hand out that
link when someone wants to work on the board alone (it needs sign-in, like the page).

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
- **Colours** (`color`, `stroke`, `fill`): palette names (gray, red, orange, yellow,
  green, blue, purple, pink) follow the light and dark theme – prefer them. A custom
  `#rrggbb` (as people pick it in the colour picker) stays the same in both themes;
  use it only when someone asks for that colour.
- **Look at it:** `whiteboard_screenshot` returns the board as a picture, drawn by
  the server from its current content like the editor shows it (hand-drawn strokes,
  the app's fonts and colours; `dark` for the dark theme). With `frame` (id or title)
  it shows only one frame or prototype screen. Use it after drawing to check
  layout, overlaps and readability; embeds, entries and blocks appear as cards, so
  read exact content with `get_whiteboard`.
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
  their bars; trees, racks and floor plan excerpts are live; mind maps, before/after and annotated images and plugin blocks are pictures
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
  "" (clean, in the app's fonts, colours and accent – the default) or `sketch` (a
  hand-drawn wireframe: only when people ask for a wireframe or rough sketch), and
  `screens`: `{name, start, overlay modal|sheet, actions, elements}`. Screens go in a row
  from left to right in the order given; a new sheet or modal is placed under the screen
  that opens it, so name the `overlay` target exactly. People see the arrows of the selected
  screen (or all, or none), and each screen has "▶ from here" to play from it.
- **Layout – let Phoedox place things, do not give `x`/`y`:** elements stack from the top
  (16 px margins, 12 px gaps); a `navbar` goes to the bottom. Containers lay out their
  `items`: `{layout: row|column|grid, items, gap, pad, cols (grid), style: card}` –
  `row` puts items side by side (items with `w` keep their width, the rest share it by
  `flex`, default 1), `grid` makes `cols` equal columns, `style: card` (or a `fill`) draws a
  surface behind the items with 16 px padding. Containers nest up to four levels (e.g. a
  grid of cards, each a column with a row inside). Long screens grow and scroll when played
  (the navigation bar stays at the bottom). `x`/`y` only for things that must float.
- **Text styles:** `{type: text, text, style: title|heading|subheading|body|caption|label}`
  sets size, weight and colour (caption and label are muted); texts are left-aligned.
- **Check:** the result lists `warnings` (elements outside a screen or covering each
  other); fix them, then look at the screens with `whiteboard_screenshot` (`frame`: the
  screen's name) before you tell people it is done.
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
    {"type": "text", "text": "Your cart", "style": "title"},
    {"layout": "row", "style": "card", "items": [
      {"layout": "column", "gap": 4, "items": [
        {"type": "text", "text": "Coffee beans", "style": "subheading"},
        {"type": "text", "text": "{{count}} × 12.90 €", "style": "caption"}]},
      {"comp": "button", "text": "+1", "variant": "secondary", "w": 64, "actions": [{"on": "click", "do": "inc", "target": "count"}]}]},
    {"comp": "button", "text": "Pay", "actions": [{"on": "click", "do": "overlay", "target": "Paid"}]}]},
  {"name": "Paid", "overlay": "sheet", "elements": [
    {"type": "text", "text": "Thank you!", "style": "heading"},
    {"comp": "button", "text": "Close", "actions": [{"on": "click", "do": "close"}]}]}]}
```

## Other tools

- `save_whiteboard` creates or replaces a whole board (e.g. an import); for changes
  use `edit_whiteboard`, for diagrams `draw_diagram`.
- Pages can show a whiteboard read-only with a ```whiteboard block (see
  `blocks/whiteboard.md`).

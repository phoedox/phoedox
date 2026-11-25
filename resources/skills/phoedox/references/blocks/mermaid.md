# ```mermaid – Mermaid diagrams

The block holds a Mermaid diagram (flowchart, sequence, state, class, gantt, …).

Mermaid ids must not be Mermaid keywords: in a `sequenceDiagram`, a participant called
`Off`, `End`, `Loop`, `Alt` or `Note` breaks the diagram (`participant Remote as Off-site`
works). Pages show such an error in the block. `validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. (common mistakes, not the
full Mermaid parser).

For diagrams people should edit by hand, draw a whiteboard instead (`draw_diagram`,
which also takes a Mermaid flowchart; see `../whiteboards.md`).

## Colours of single blocks

People edit most diagram kinds in a form in Phoedox, which reads and writes these
conventions; use them so the form keeps the colours:

- **Flowchart / swimlane nodes:** palette colours (gray, red, orange, yellow, green, blue,
  purple, pink) as `classDef phx-<colour> fill:…,stroke:…` plus `class a,b phx-<colour>`;
  Phoedox replaces the fill with the theme colour (light and dark). Other colours:
  `style a fill:#rrggbb,stroke:#rrggbb,color:#000000` (text colour for contrast).
  Mermaid does not accept `var(...)` in `style`.
- **Gantt bars** have no classes: colour them with CSS in the front matter, one rule per task
  id (the task needs an id):

  ```
  ---
  config:
    themeCSS: "rect[id$='-a2'] { fill: var(--tb-green, #dcefe1) !important; stroke: var(--tc-green, #2c8a4b) !important; } text[id$='-a2-text'] { fill: var(--tc-green, #2c8a4b) !important; }"
  ---
  gantt
      ...
  ```

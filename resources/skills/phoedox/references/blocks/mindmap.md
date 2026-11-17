# ```mindmap – mind maps

This block comes from the official plugin `com.phoedox.mindmap` (ADR-0087). `get_project` lists it in
`pluginBlocks`; with `missing: true` the plugin is not active in the project yet, so the
block keeps its source until a project owner or administrator enables it (see `blocks/plugins.md`).

An indented outline: the first line is the centre; lines indented under a line (spaces or
`- ` lists, the same step throughout) are its branches. At most 400 items. Readers fold
branches and can copy the map onto a whiteboard as shapes.

````markdown
```mindmap
Moving house
  Flat
    Give notice
    Handover with protocol
  Internet
    Move the contract
  Boxes
    Kitchen
    Cellar
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. On whiteboards it is a picture; for editable diagrams use
`draw_diagram`.

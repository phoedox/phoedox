# ```tree – trees from the knowledge graph

`root: @entry` and the `relation` to follow; the entries are laid out top-down and open
when clicked. `direction: in` (default) collects entries whose relation points *to* the
root – switches and devices `wired_to` the router (or `uplink` over Wi-Fi), people with
`reports_to` their lead; `direction: out` follows the relation from the root (e.g.
`parent_of`). `depth` 1–8 (default 4). Without a relation every relation is followed.
Check `get_data_model` for the relations the project allows.

````markdown
```tree
title: Home network
root: @router
relation: wired_to
direction: in
```
````

It only names entries and documents and always shows the current state with live
status; `validate_blocks` warns when a named entry, floor plan or room does not exist.
Published pages leave it out (entries stay private).

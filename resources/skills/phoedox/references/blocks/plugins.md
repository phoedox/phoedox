# Blocks from plugins

Plugins can add block kinds (ADR-0086). `get_project` lists them as `pluginBlocks` with
`kind`, `label`, the `plugin` and an `example` of the source. Write them as fenced
blocks like core blocks (```` ```<kind> ````); the format is the plugin's – start from
the example, and read the plugin's own description when it has one.

- The plugin draws the block on the server, so it shows in the web app, the mobile app,
  published pages and on whiteboards (`{type: block, kind, source}`) where the plugin is
  enabled; without the plugin the block shows its source.
- `validate_blocks`, `create_document` and `update_document` report the plugin's findings
  like those of core blocks.
- Some plugin blocks are interactive in the web app and the mobile app.
- Some kinds moved from Phoedox into official plugins (ADR-0087), first `gantt`. Where
  the plugin is not active, `pluginBlocks` marks the kind `missing: true`: existing
  blocks keep their source and show which plugin they need, and Phoedox installs and
  enables the plugin by itself when the store can be reached (ADR-0088).
  `validate_blocks` then warns instead of checking the format. You may still write such
  a block, but tell the user that a project owner or administrator has to enable the
  plugin before it shows as a picture.

**Schedules (Gantt)** come from the official plugin `com.phoedox.gantt` (block `gantt`):
one line per item, `Name [id]: start, end or duration, …`. Start is a date
(`2026-03-01`) or `after id` (several ids: `after a b`); then an end date, a duration
(`3d`, `2w`, `1m`) or nothing for a milestone; optionally `done`, `50%` and a colour. A
range can also be written `2026-02-02 – 2026-02-06`. `## Section` groups items, `today`
draws today's line, `board: slug` adds the board's cards that have a due date where the
block is shown interactively.

````markdown
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
````

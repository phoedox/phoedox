# ```poll – polls

This block comes from the official plugin `com.phoedox.poll` (ADR-0087). `get_project` lists it in
`pluginBlocks`; with `missing: true` the plugin is not active in the project yet, so the
block keeps its source until a project owner or administrator enables it (see `blocks/plugins.md`).

The first plain line or `question:` is the question, `- …` lines are the options (2–30,
no duplicates). Flags (each also on a line of its own): `multiple` allows several
answers (percentages are then of the people who voted), `closed: yes` ends voting,
`results: always` shows results before voting. Give `id: …` when the poll may be moved
or its text edited, so votes stay attached.

Each signed-in reader votes, changes or takes back their vote; results appear as bars
after voting. Votes are stored per person; nobody sees who voted what.

````markdown
```poll
question: Where do we go on the family trip?
id: trip-2026
- Zoo
- Museum
- Swimming pool
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. On whiteboards it can be used right on the board.

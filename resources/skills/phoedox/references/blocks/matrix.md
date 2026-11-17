# ```matrix – decision matrices

This block comes from the official plugin `com.phoedox.matrix` (ADR-0087). `get_project` lists it in
`pluginBlocks`; with `missing: true` the plugin is not active in the project yet, so the
block keeps its source until a project owner or administrator enables it (see `blocks/plugins.md`).

`criteria:` lists criteria with an optional weight (`Price*3`), `scale: 1-5` the best
score, `- Option: score, score, …` one score per criterion, higher is better (score cost
so that cheaper is higher). The total is the weighted sum as a share of the best
possible; the best option is highlighted. Readers can try other weights (not saved) and
turn the matrix into an open decision (an ADR with the options) themselves.

Use it to prepare a decision; for the decision itself use `create_decision` with options
(`../decisions.md`).

````markdown
```matrix
title: Which NAS do we buy?
criteria: Price*3, Noise*2, Storage*3, Power*1
scale: 1-5
- Synology DS224+: 3, 4, 3, 4
- QNAP TS-264: 4, 3, 4, 3
- Self-built: 5, 2, 5, 2
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. On whiteboards it shows its bars.

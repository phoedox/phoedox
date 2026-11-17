# ```mermaid – Mermaid diagrams

The block holds a Mermaid diagram (flowchart, sequence, state, class, gantt, …).

Mermaid ids must not be Mermaid keywords: in a `sequenceDiagram`, a participant called
`Off`, `End`, `Loop`, `Alt` or `Note` breaks the diagram (`participant Remote as Off-site`
works). Pages show such an error in the block. `validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. (common mistakes, not the
full Mermaid parser).

For diagrams people should edit by hand, draw a whiteboard instead (`draw_diagram`,
which also takes a Mermaid flowchart; see `../whiteboards.md`).

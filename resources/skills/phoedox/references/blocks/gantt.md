# ```gantt – schedules

One line per item, `Name [id]: start, end or duration, …`. Start is a date
(`2026-03-01`) or `after id` (several ids: `after a b`); then an end date, a duration
(`3d`, `2w`, `1m`) or nothing for a milestone; optionally `done`, `50%` and a colour. A
range can also be written `2026-02-02 – 2026-02-06`. `## Section` (or `section Name`)
groups items, `today` draws today's line, `board: slug` adds the board's cards that have
a due date (from the day they were created to the due date). Dependencies are drawn as
arrows.

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

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. (bad dates, unknown `after` ids, cycles).

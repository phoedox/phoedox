# ```chart – charts and annotated timelines

A ```chart block holds one JSON object. Use it for numbers over time or per thing,
and for **annotated timelines**: a curve with numbered events, phases behind it and
the events listed with their text under the chart (hovering a point shows it too).

| Field | Meaning |
|---|---|
| `type` | `line` (default), `area`, `bar`, `scatter` |
| `title`, `description`, `height` | heading, a sentence under it, plot height in px (160–800, default 300) |
| `points` or `series` | one series as `points`, or `series: [{name, color, curve: smooth\|straight\|step, dashed, points}]` |
| point | `{x, y, label, title, text, color}` – `x` a date (`1848`, `1848-03`, `1848-03-18`), a number or a name (bars); `y` a number or `null` (a gap); `label` is drawn in the marker (e.g. its number); points with `title`/`text` are listed under the chart |
| `x` | `{label, type: time\|number\|category, segments: [{from, to, weight, label}]}` – segments sit side by side, each with its own scale: compress quiet decades and stretch busy years (`weight` = share of the width); a dotted line marks the border |
| `y` | `{label, min, max, unit, ticks}` – `ticks` may carry words: `[{value: 0, label: "low"}, {value: 10, label: "high"}]` for qualitative scales |
| `bands` | phases behind the plot: `[{from, to, label, color}]`; points without a colour take their phase's colour, and the legend lists the phases |
| `notes` | short texts on the plot: `[{x, y, text, color}]` |
| `list` | `false` hides the numbered list |

Colours are the board palette: gray, red, orange, yellow, green, blue, purple, pink
(they follow light and dark mode). A dashed second series continues a curve
("ideas live on"). `validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`.

````markdown
```chart
{"title": "Pressure on the old powers",
 "x": {"segments": [{"from": "1815", "to": "1847-12", "label": "compressed"},
                    {"from": "1848-01", "to": "1849-10", "weight": 1.2, "label": "stretched"}]},
 "y": {"ticks": [{"value": 0, "label": "low"}, {"value": 5, "label": "medium"}, {"value": 10, "label": "high"}]},
 "bands": [{"from": "1815", "to": "1848-02", "label": "Restoration", "color": "orange"},
           {"from": "1848-02", "to": "1848-05", "label": "March revolution", "color": "red"},
           {"from": "1848-05", "to": "1849-10", "label": "Counter-revolution", "color": "gray"}],
 "series": [{"points": [
   {"x": "1815", "y": 1, "label": "1", "title": "1815 – Congress of Vienna", "text": "Restoration."},
   {"x": "1832", "y": 4, "label": "2", "title": "1832 – Hambach Festival"},
   {"x": "1848-03-18", "y": 9.5, "label": "3", "title": "March 1848", "text": "Barricades in Berlin."},
   {"x": "1849-07", "y": 1, "label": "4", "title": "July 1849 – the revolution ends"}]},
   {"dashed": true, "points": [{"x": "1849-07", "y": 1}, {"x": "1849-10", "y": 2.5}]}],
 "notes": [{"x": "1849-09", "y": 3, "text": "ideas live on"}]}
```
````

On whiteboards: `{type: block, kind: chart, source: <the JSON>}` (about 560×360).

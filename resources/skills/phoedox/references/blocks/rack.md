# ```rack – a rack by height unit

The block holds the rack's entry. Devices appear by their `mounted_in` relation with `u`
(lowest height unit, 1 = bottom) and `height` on the relation; the rack's `height_units`
attribute sets its size (default 42). Overlaps and devices without a position are shown.

````markdown
```rack
@rack-basement
```
````

It only names entries and documents and always shows the current state with live
status; `validate_blocks` warns when a named entry, floor plan or room does not exist.
Published pages leave it out (entries stay private).

# ```status – live state tiles

One `@entry` per line, `checks` for all checks of the project, `check: Name` for one.
Shows a tile per entry or check with its current state (ok, warning, down) and the
reason; it refreshes while the page is open.

````markdown
```status
@router
@nas
check: Backup ran
```
````

Published pages show only the block's text (no live state).

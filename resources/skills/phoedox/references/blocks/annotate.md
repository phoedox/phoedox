# ```annotate – images with numbered markers

`image: <image> description`, then one line per marker: `- x, y: text` with x and y in
percent of the picture (0–100, from the top left), optionally a colour after the position
(`- 74, 64 blue: RCD`). `@entry` in a text links the entry. Markers are numbered in order
and listed under the picture. People can also place markers by clicking in the editor.
Good for a fuse box, the back of a router, a rack.

**Images** are files of the project (`/api/v1/files/<id>`, see `list_files` and
`upload_image`) or https addresses. Attach the file to the page so published pages show
it. Words after the address are the label.

````markdown
```annotate
title: Fuse box
image: /api/v1/files/01JQ… Fuse box in the hall
- 22, 30: F1 kitchen and @stove
- 48, 30: F2 bathroom
- 74, 64 blue: RCD, test monthly
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. (positions, colours, image addresses).

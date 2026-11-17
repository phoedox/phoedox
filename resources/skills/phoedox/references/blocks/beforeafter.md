# ```beforeafter – before/after photos

`before: <image> label` and `after: <image> label` (or two Markdown images), optional
`title:` and `start: 30` (slider position in percent). The photos lie on top of each other
with a slider between them (keyboard too); print shows both side by side. Good for a
renovation, tidied-up cabling, damage and its repair.

**Images** are files of the project (`/api/v1/files/<id>`, see `list_files` and
`upload_image`) or https addresses. Attach the file to the page so published pages show
it. Words after the address are the label.

````markdown
```beforeafter
title: Rack cabling
before: /api/v1/files/01JQ… Before
after: /api/v1/files/01JR… After
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`.

# Built-in app kinds

Ready-made apps people pick under New → App (and AI clients with
`create_app` `starter`): each folder holds the app's files and a
`manifest.json` (level, sources, storageMb, selfEdit). They use only the
bridge and the fixed libraries; no network.

| Folder | What it is |
|---|---|
| `slides` | Slides from Markdown (`slides.md`, `---` between slides): present full screen, print to PDF, editors change them in place |
| `dashboard` | Tiles, a chart and a table for every source of the manifest (checks to start with) |
| `sheet` | A spreadsheet people fill in together (formulas, live, CSV download) |
| `design` | The project's theme as a design system page: colours, type, components, contrast |

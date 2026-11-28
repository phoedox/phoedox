# ```familytree – family trees

This block comes from the official plugin `com.phoedox.genealogy` ("Family tree"). `get_project`
lists it in `pluginBlocks` when the plugin is active in the project. The plugin also adds the
kinds `relative`, `place`, `parish` and `source` and its own tools (`plugin_genealogy_*`):
read `plugin_genealogy_guide` before filling a family tree, `plugin_genealogy_gaps` shows what
is missing, `plugin_genealogy_add_family` writes people, relations and sources in one call.

The block draws the ancestors or the descendants of one relative:

````markdown
```familytree
@lena-mueller-1985
view: ancestors
generations: 4
title: Lena's ancestors
```
````

- The first line names the relative (`@slug`, or `person: @slug`).
- `view: ancestors` (parents above) or `view: descendants` (children below, partners beside).
- `generations: 1–8` (default 4), `title:` optional. German keys work too (`ansicht`,
  `generationen`, `titel`).

The picture is drawn from the knowledge graph and stays current. Outside the project
(publications, shared pages, guests) living people appear as "living person" without data,
or are left out, as the project owner set it. `validate_blocks` checks the block.

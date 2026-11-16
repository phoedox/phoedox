# Pages

Pages are documents of type `markdown`. What they can contain (links, callouts,
request blocks, live blocks, diagrams) is in `page-syntax.md`.

## Read

- `list_documents` (optionally `type`, `category`) lists documents without content.
- `get_document` returns the published version; if someone is editing the page, the
  open draft comes as `draft` too – your writes go into it.
- **Long pages:** `get_document` with `heading` reads one section – a heading's text
  or its number (`"6.4"`, `"§6.4"`, `"Backups"`); the section runs to the next
  heading of the same or a higher level. An unknown heading returns the page's
  `outline`, so you can pick the right one.
- `list_document_versions`, `get_document_version`, `restore_document_version` for
  the history of one page (restoring stores a new version).
- `snapshots`: named points in the project history, e.g. the docs at release
  0.27.4 (release tags of a connected repository become snapshots by
  themselves). Without name it lists them; with name it shows the documents as
  they were then (read one with `get_document_version`); `create: true` names
  the current point; `compare_to` lists what changed since another snapshot or
  "now" – useful for release notes of the documentation.

## Write

- `create_document` with title, Markdown and `category` (a `/` makes a folder).
  `after` (slug or id of a page) puts it right after that page in its folder; without
  a category it goes into that page's folder. `translations` saves translations in
  the same call (see `languages.md`).
- `update_document` changes only the fields you send:
  - `markdown` replaces the whole text – read the page first.
  - `edits` changes passages: a list of `{old, new}` applied in order; each `old`
    must occur exactly once (add surrounding words until it does). All or nothing.
    Prefer this for small changes to long pages. For decisions add `field`
    (`context`, `decision`, `consequences`, `alternatives`); for links the
    description is edited.
  - `title`, `category`, `slug`, `position`, `after`. Moving a page (folder, order,
    slug) creates **no new version** and keeps translations current.
  - `expect_version` fails when the page changed since you read it.
- Answers are short: `applied`, `document` (id, slug, title, version, url), `draft`
  or `suggestion`, and `blockProblems` for mermaid and dbmodel blocks. Fix the
  blocks it names. `verbose: true` returns the whole document.
- `delete_document` moves a document to the trash.

```json
{"project": "home", "document": "backup",
 "edits": [{"old": "keeps 30 days", "new": "keeps 90 days"},
           {"old": "## Restore\n\nAsk Alex.", "new": "## Restore\n\nSee [[restore-a-backup]]."}]}
```

## Many changes at once

`batch_update` takes up to 100 operations, applied in order:

- `{action: "update", document, title?, markdown?, edits?, category?, position?, after?, expect_version?}`
- `{action: "translate", document, lang, title?, markdown?, edits?, context?, decision?, …}`

Every operation is checked first; if one is invalid, nothing is written and the
error names it. Use it for renames, moving many pages into folders, or a new title
plus its translations. Combine changes to the same document into one operation.

```json
{"project": "app", "operations": [
  {"action": "update", "document": "auth", "title": "Sign-in"},
  {"action": "translate", "document": "auth", "lang": "de", "title": "Anmeldung"},
  {"action": "update", "document": "sessions", "category": "Architecture/Backend", "after": "auth"}]}
```

## Find and replace

- `search` ranks pages and entries by words – for "where is something about X".
- `find_in_documents` finds **every** occurrence of an exact text or a regular
  expression (`regex: true`, RE2 syntax), with line and surrounding text: in page
  text (the open draft if there is one), decision texts and link descriptions,
  optionally `titles`; in one, several (`documents`) or all documents; in the main
  language or one translation (`lang`). Options: `case_sensitive`, `whole_word`.
- `replace_in_documents`:
  1. Call it with `apply: false` (the default): the preview lists every match with
     its number and replacement. Show the user what will change.
  2. Call it again with `apply: true`, the same search, `versions` (document id →
     version from the preview; documents changed since are skipped) and, to leave
     matches out, `only` (document id → match numbers to replace; an empty list
     leaves the document out).
  Each changed document gets a normal version (or becomes a suggestion, or goes into
  the open draft). With `regex`, `$1` in the replacement inserts a group.
- Replacing inside code blocks, links or `@mentions` changes them too – check the
  preview for those.

## Check diagrams

`validate_blocks` checks the mermaid and dbmodel blocks of a page (its draft if
open) or of Markdown you are about to save: unknown diagram types, Mermaid keywords
used as names, unclosed blocks, unbalanced brackets or quotes, tables that no dbmodel
block of the project defines. The Mermaid check catches common mistakes, not
everything the renderer rejects – the page shows the rest in the block.

## Links, files and pictures

- `create_link` makes a document of type `link` (external documentation);
  `archive_link` keeps a copy of what it points to.
- `list_files` lists the project's files (no contents).
- `upload_image` sends a picture as base64 (PNG, JPEG, GIF, WebP or SVG; SVG is
  cleaned of scripts, event handlers and outside links) and attaches it to a page,
  an entry or a task card; it returns the Markdown to insert. `set_project_image`
  sets the project's logo. Both need the project's AI write mode "direct".

## Comments

`list_comments`, `add_comment` (on a page, on a block via `anchor`, or as a reply
via `parent`). Write findings into the page or a comment, not only into the chat.

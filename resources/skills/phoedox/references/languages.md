# Languages

A multilingual project has a main language and further languages (`get_project`:
`mainLocale`, `locales`). One document holds its text in the main language and one
translation per further language; number, slug, folder, status and links belong to
the document.

## Rules

- Write new content in the **main language**. Translations keep `@mentions`, links,
  code and `{{variables}}` unchanged.
- Your translations are shown as machine translations until a person reviews them.
  Set `original: true` only when the text is the user's own in that language (for
  example importing their German originals) – never for a translation you made.
- A translation is **outdated** when the main text changed after it. Changing only
  status, links, folder, order or other metadata keeps translations current; moving
  a page creates no new version at all.

## Read and write

- `get_document` with `lang` returns the translation; `translation.sourceVersion`
  lower than `version` means it is outdated.
- `translate_document` writes one language:
  - Fields you leave out **keep** their translated text; only an empty string
    clears one. `title` may be left out when the translation exists.
  - `edits` (`{old, new}`, exactly once; `field` for decisions) changes passages of
    the existing translation.
  - With text or edits the translation counts as current.
  - **Title only** (after renaming in the main language): send just the translated
    `title`. The text stays, and the translation counts as current when the main text
    did not change since – the answer says `outdated`.
- `create_document`, `create_decision` and `create_decisions` take `translations`
  to save them in the same call.
- `batch_update` with `translate` operations translates many titles or texts in one
  call (see `pages.md`).
- The scope of a partial supersede is translated with `supersedes_scope`.

## Find what needs work

`list_documents` shows `translationState` per further language: `current`,
`outdated` or `missing`. `translation: "stale"` lists only documents that need work
(outdated or missing; `"outdated"` or `"missing"` for one kind), `lang` limits it
to one language.

## Folder names

`translate_category` names a folder in a further language – the last part of the
path only (`Architecture/Backend` → "Hintergrund"), so the sidebar shows it in that
language. An empty name removes it. Only where AI writes directly.

## Find and replace in a translation

`find_in_documents` and `replace_in_documents` with `lang` work on that language's
translations; replacing there keeps the translation's state (it does not become
current by it).

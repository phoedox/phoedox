# Decisions (ADRs)

Decisions are documents of type `adr`, numbered per project (ADR-0001 …). Phoedox
assigns the number and never reuses it; you can use it wherever a document is
expected. Use decisions for decisions – not Markdown pages.

## Record a decision

1. `list_decisions` (optionally `query`, `status`) – is it already decided, or does a
   new one replace an old one?
2. `create_decision` with `context`, `decision`, `consequences` and `alternatives`,
   in the main language; `status` accepted unless people still have to decide.
3. A decision that replaces an older one sets `supersedes` (number or slug). Phoedox
   marks the older one as superseded – do not change its status yourself. If it
   replaces only part of it, also set `supersedes_partly` and `supersedes_scope`
   (which part); the older one becomes `partially_superseded` and stays valid
   otherwise.
4. `update_decision` changes fields later; for small text changes use
   `update_document` with `edits` and `field`.

## Prepare a decision for people

When a person has to choose (the user asks "what are my options?", or you are not
allowed to decide), do not pick one yourself:

1. `create_decision` with `status: proposed`, the context, and `options`: each
   option has a `title` and a Markdown `description` with pros, cons and cost. The
   `decision` field stays empty; Phoedox assigns option ids (`o1`, `o2`, …).
2. Tell the user to decide on the page: they pick an option (the decision becomes
   `accepted`, the option its decision text, the others its alternatives) or answer
   "nothing fits", each with an optional comment. You never decide.
3. Later, `list_decisions`: `needsOptions: true` means the last answer was "nothing
   fits". Read the `responses` with `get_document` (the comments say what is
   missing) and offer new options with `update_decision` (`options` replaces the
   list; the responses stay).

## Import many decisions

- `create_decisions` takes up to 50 at once; each item reports its own result, and an
  item may supersede an earlier one of the same call.
- `ext_ref` keeps a decision's own number from another system ("D-640"): links like
  `[[D-640]]`, `search`, `get_document("D-640")` and every tool that takes a
  document find it, while Phoedox still assigns ADR numbers.
- `translations` (language → title and texts) saves translations in the same call;
  set `original: true` there only for the user's own text in that language.

```json
{"project": "app", "decisions": [
  {"title": "Use SQLite on devices", "ext_ref": "D-101", "status": "accepted",
   "context": "…", "decision": "…",
   "translations": {"de": {"title": "SQLite auf Geräten", "decision": "…", "original": true}}},
  {"title": "Sync over WebSocket", "ext_ref": "D-102", "status": "accepted", "decision": "…",
   "supersedes": "D-101", "supersedes_partly": true, "supersedes_scope": "only the sync part"}]}
```

# Apps

An app is a document of type `app`: a small web program that lives in the
project, made mostly by the AI. People open it full screen, use it on its
page and embed it in pages. Use an app when people want to **use** something
(dashboard, checklist, form, calculator, clickable prototype); for reading,
write a page.

## Files

- `index.html` (plain HTML/CSS/JS), or `app.tsx` / `app.jsx` (React) –
  Phoedox makes the page around it with `<div id="root">`.
- `.ts`, `.tsx`, `.jsx` are compiled on save (no build step, no npm).
- Libraries by import name only: `react`, `react-dom/client`,
  `react/jsx-runtime`, `lit`, `chart.js`, `d3`.
- Up to 40 files, 1 MB together.
- The frame has **no network**: no `fetch` to other sites, no external
  scripts, fonts or images (use inline SVG or data URLs). The app's own files
  can be fetched by relative path.
- Light and dark: the frame's `<html data-theme="light|dark">` follows
  Phoedox; style both.

```tsx
// app.tsx
import { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";

function App() {
  const [rows, setRows] = useState<any[]>([]);
  useEffect(() => { phoedox.read("servers").then((r) => setRows(r.rows)); }, []);
  return <ul>{rows.map((r) => <li key={r.slug}>{r.name}</li>)}</ul>;
}
createRoot(document.getElementById("root")!).render(<App />);
```

## The data bridge

The app never sees a token. It calls `window.phoedox`; the Phoedox page
around it checks every call against what the person allowed. The manifest
says the highest **level** the app asks for; each person grants it once.

| Level | Calls | Manifest |
|---|---|---|
| 0 | `info()` only | – |
| 1 | `read(sourceId)` | `sources: [{id, kind, ref, label}]` – kind `query` (saved query name), `board` (board slug), `checks` (optional page), `entities` (kind) |
| 2 | `storage.get/set/delete/list(key, value, {personal})` – JSON values, shared by default, `personal: true` per person | `storageMb` (default 10, counts against the project storage) |
| 3 | `propose({kind: "task", board, title, description, column})`, `propose({kind: "comment", document, body})`, `propose({kind: "entity", entityKind, name, attrs})` – the person confirms each change | – |

**AI:** with `ai: true` in the manifest, `phoedox.ai(prompt)` returns the AI's
text and `phoedox.ai(prompt, {json: true})` parsed JSON. It runs on the budget of
the person using the app, only where the organization allows the AI chat.

`requestAccess()` asks the person for the level; `info()` returns
`{level, granted, sources}`. Reads run with the viewer's rights, never more.
Ask for the **lowest level** that does the job and explain to the user what
the app will be allowed to do.

## Working on apps

1. `create_app` with `files`, `level`, `sources`.
2. `get_app`: compile errors (`app.build.errors`) and the last console errors
   of people using it (`console`). Fix them with `edit_app`.
3. `edit_app` with exact `edits` `{file, old, new}` (old exactly once), whole
   `files` (null removes one) or a new `manifest`.

Every save is a version (restore with `restore_document_version`).

## Apps from store templates

People can create an app from a reviewed template of the Phoedox store
(New → App from the store). Such an app has `app.origin` (store item id and
version) in `get_app`. Updating it to a newer template version replaces its
files (its stored data stays), so when you change such an app with
`edit_app`, tell the user that a later template update would overwrite those
changes. Organizations can limit who may install themes and app templates
from the store; people without access send a request to the organization's
admins. Point users there instead of copying a template's code by hand.

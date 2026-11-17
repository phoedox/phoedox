# Apps

An app is a document of type `app`: a small web program that lives in the
project, made mostly by the AI. People open it full screen, use it on its
page and embed it in pages. Use an app when people want to **use** something
(dashboard, checklist, form, calculator, a high-fidelity prototype in code);
for reading, write a page. For clickable screens people want to see and
change on a whiteboard (flows, mock-ups), draw a prototype with
`draw_screens` instead (`whiteboards.md`); people can turn one into an app
later with "Make it an app", which creates an app whose `index.html` plays
the prototype – build on that file with `edit_app`.

## Files

- `index.html` (plain HTML/CSS/JS), or `app.tsx` / `app.jsx` (React) –
  Phoedox makes the page around it with `<div id="root">`.
- `.ts`, `.tsx`, `.jsx` are compiled on save (no build step, no npm).
- Libraries by import name only: `react`, `react-dom/client`,
  `react/jsx-runtime`, `lit`, `chart.js`, `d3`.
- Up to 40 files, 1 MB together.
- The frame has **no network**: no `fetch` to other sites, no external
  scripts, fonts or images. The app's own files can be fetched by relative
  path.
- **Assets** (pictures, fonts, sounds, data files up to 25 MB each):
  `upload_app_asset` with base64, then use `assets/<name>` in the code
  (`<img src="assets/logo.png">`, `fetch("assets/data.json")`). `get_app`
  lists them; `copy_app_assets` copies them from another app (also from
  another project you can read). Both need the AI write mode "direct".
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
| 1 | `me()` – `{id, name, editor}` of the person using it; the id is stable within this app and is not their account id. `read(sourceId)` | `sources: [{id, kind, ref, label}]` – kind `query` (saved query name), `board` (board slug), `checks` (optional page), `entities` (kind), `request` (`page#n`: the n-th request block of a page, GET/HEAD only, with the project's variables), `readings` (`entry` or `entry:metric`: live values and their history) |
| 2 | `storage.get/set/delete/list(key, value, {personal})` – JSON values, shared by default, `personal: true` per person. `db.collection(name, {personal})` – records: `add(data)`, `put(id, data)`, `update(id, fields)` (null removes a field), `delete(id)`, `query({where: [[field, op, value]], order, desc, limit, offset, mine})` (ops `= != < <= > >= in contains`), `subscribe(f)` – live changes from everyone. `files.upload(file, name)`, `files.list()`, `files.get(id)` (a Blob), `files.delete(id)` – files people add (photos, receipts) | `storageMb` (default 10, counts against the project storage; records and files count too) |
| 3 | `propose({kind: "task", board, title, description, column})`, `propose({kind: "comment", document, body})`, `propose({kind: "entity", entityKind, name, attrs})` – the person confirms each change | – |

**AI:** with `ai: true` in the manifest, `phoedox.ai(prompt)` returns the AI's
text and `phoedox.ai(prompt, {json: true})` parsed JSON. It runs on the budget of
the person using the app, only where the organization allows the AI chat.

**Without a level:** `download(name, data, type)` offers a file to save (text,
JSON or a Blob; the person confirms), `open(ref)` opens a page, entry or card.

**Device features:** list them in the manifest's `devices` (`camera`,
`microphone`, `screen`, `location`, `motion`). Each person allows **each
feature separately** (and can change it under Permissions on the app's
page); admins can switch device features off. `device.status()` tells what
is allowed, `device.request(feature)` asks. For camera, microphone and
screen the frame reloads once allowed – call `request` again on start and
then use the browser API (`getUserMedia`, `getDisplayMedia`).
`device.location()` returns `{lat, lng, accuracy}`, `device.onMotion(f)`
reports motion about ten times a second. Ask only for the features the app
really needs and say why in the app.

**Apps that change themselves:** with `selfEdit: true` (create_app
`self_edit`), `self.files()` returns the app's files and `self.save(files)`
saves new ones (null removes a file) as a new version – only for people who
may edit the app, after they confirm. Use it for apps people extend
themselves (a form builder, a settings page that rewrites its config).

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

4. `app_screenshot`: a picture of the app as it looks (PNG). It is taken
   when someone who may edit the app has it open in the browser after a
   change; the note says which version it shows. If it is older than the
   current version, ask the person to open the app once.

Every save is a version (restore with `restore_document_version`).

## Comments and watching people work

- People comment on apps like on pages. A comment with **@AI** (or @KI,
  @Claude) is answered by Phoedox's background AI, which may also change the
  app as asked (as a suggestion where AI writes are suggestions).
- When the user asks you to watch a page or app while they try it, use
  `wait_for_changes`: once without `cursor` to get one, then with `cursor`
  and `wait` (up to 50 s) in a loop. It returns what people changed or
  commented (not your own writes) with the comment text, so you can answer
  (`add_comment` with `parent`) or fix the app. Stop when the user says so
  or nothing happens for a while.

## Gallery and copies

The project's Apps page (tools menu) shows all apps with a picture. People
can make a copy of an app (into the same or another project; data and grants
are not copied) and, for public apps, a signed-in visitor can copy it into
their own project. A public app's link shows its title and picture in chats
and social networks.

## Apps from store templates

People can create an app from a reviewed template of the Phoedox store
(New → App from the store). Such an app has `app.origin` (store item id and
version) in `get_app`. Updating it to a newer template version replaces its
files (its stored data stays), so when you change such an app with
`edit_app`, tell the user that a later template update would overwrite those
changes. Organizations can limit who may install themes and app templates
from the store; people without access send a request to the organization's
admins. Point users there instead of copying a template's code by hand.

## Where apps show up

- On pages with an ```app block holding the app's slug (`page-syntax.md`).
- On whiteboards as an embed `{kind: app, doc: <slug>}` (`whiteboards.md`).
- On task cards: link the app document to the card (`documents` of the task);
  people start it inside the card.

Everywhere the app runs with the rights and grants of the person looking at
it.

## Public apps

Project owners can release an app to people without an account (the app's
page → Share publicly). Visitors never reach the project: the app runs at
level 0, or – in snapshot mode – its `read()` calls are answered from a
snapshot of its sources that the owner took with their own rights and
checked in a preview first. The snapshot is refreshed by hand or on a
schedule (hourly, daily, weekly). Storage, proposals and `open()` are
refused for visitors; `ai()` works only within a monthly limit the owner
sets, on the owner's budget, behind the bot check. Admins of the instance
or the organization can forbid public apps. There is no MCP tool for this:
publishing is a step a person takes in Phoedox. When you build an app meant
to be shared publicly, keep it to level 1 and make it work with data that
is not live: for visitors `info()` also returns `public: true` and
`snapshotAt` (the time of the data), which the app can show.

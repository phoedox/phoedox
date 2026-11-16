# Boards and tasks

A board is a document of type `board` with columns; its cards are tasks.

## Boards

- `create_board` (title, category, columns) and `update_board` (title, folder,
  description above the board, columns: rename, reorder, add; only empty columns can
  be removed – keep the ids of existing columns).

## Cards

1. `list_tasks` (optionally `board`, `column`, `assignee`, `open`) to avoid
   duplicates; `my_tasks` lists what is assigned to the token owner.
2. `create_task` with a clear title, Markdown description, `labels`, `priority`
   (0–3), `due` (YYYY-MM-DD), `assignee`, and `documents` for the pages and
   decisions the card belongs to (e.g. `["auth-concept", "ADR-0012"]`).
3. `update_task` changes only what you send; moving a card to the done column
   completes it. `documents` replaces the linked list.

**Subtasks** are the card's checklist: `create_task` takes titles in `subtasks`;
`update_task` replaces the whole list, so keep the ids from `list_tasks` and tick
items with `done: true`. Markdown checklist lines (`- [ ] step`) in a description
become subtasks by themselves, and `[[page]]` links in a description link the page to
the card.

**Time:** `log_time` records time spent on a card ("1:30", "45m") for the signed-in
person – only when the person tells you how long they worked.

## Code on cards

When a card is about code and the project has repositories (`list_code_repos`, GitHub
or Gitea):

- After the work is pushed, find the commit with `search_commits` (by SHA or message)
  and link it with `link_task_code` (`kind: commit`, the SHA), or link the pull
  request (`kind: pull`, its number).
- Do not link releases by hand: Phoedox adds the first release that contains a linked
  commit. `list_tasks` shows the links as `codeLinks`.
- Connecting a repository is for project owners in the settings.

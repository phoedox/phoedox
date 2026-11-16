# Feedback portals

Projects can run **feedback portals**: public pages at `/feedback/<handle>` (plus a widget
and an API) where people report bugs, ideas, questions and praise about what the project
builds – a game, a website, a system. Reports that describe the same thing are grouped into
**topics**; moderators turn topics into **tickets** (cards on boards).

## Building blocks

| Block | What it is |
|---|---|
| Portal | handle, name, report types, categories, own typed form fields, topic states, attachment rules, who may report |
| Report | one submission: type, title, text, form fields, context, attachments, reporter (account, anonymous, app or server key) |
| Topic | reports about the same thing: title, summary, category, tags, priority 1–4, state, answer to reporters, number of reports |
| Ticket | a card on a board made from a topic; a topic can have several |

## Moderating (editors, owners, or moderators of a portal)

1. `list_feedback_topics` – topics, highest priority first; filter by `portal`, `state`,
   `category`, `priority`, `q`.
2. `get_feedback_topic` – a topic with all its reports, the portal's settings (states,
   categories) and its tickets.
3. Triage with `update_feedback_topic`:
   - write a short **summary** in the project's main language;
   - set **category** and **tags** only from the portal's lists (never invent new ones);
   - **priority**: 1 urgent (crash, data loss, security, many reporters), 2 high, 3 normal, 4 low;
   - **state** from the portal's states.
   - `answer` is shown to the reporters and sent to them – set it only when a person asked
     for that answer.
4. Duplicates: `merge_feedback_topics` (into = the topic that stays, from = the duplicate).
5. Into work: `feedback_to_board` creates a card on a board linked to the topic. Make one
   ticket per team or part (e.g. "Frontend" and "Backend"). When all tickets of a topic are
   done, the topic is done and the reporters are told automatically.

## Reporting

- `get_feedback_portal` returns a portal's report types and own fields (ids, types, options,
  required).
- `submit_feedback` sends a report as the signed-in person – e.g. when someone describes a bug
  in the chat and wants to report it. Fill the required fields; secrets in the text are removed.

## Good to know

- Reporters never see each other; moderators never see IP addresses.
- Attachments count against the project's **feedback storage**, a quota of its own
  (project settings → Usage & quotas).
- Anonymous reports pass the bot protection; apps send reports with a public key (also with the
  bot check) or from their backend with a server key.
- Owners set portals up under project settings → Feedback portals; people point others to
  `/feedback/<handle>`.

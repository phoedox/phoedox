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
| Topic | reports about the same thing: title, summary, category, tags, priority 1–4, state, answer to reporters, number of reports; with AI triage also `aiNote` (why), `duplicateOf` (a topic the AI takes for the same issue) and `spam` |
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
   - `fixed_in`: the product version that fixed it (reporters are told);
   - `entities`: the knowledge-graph entries the topic is about (slugs); entry pages then
     list the topic for moderators. The AI triage may already have suggested some.
   - `question`: asks the reporters for more information; they answer on their status
     link and the answers appear under their reports (`replies` in `get_feedback_topic`).
     Like `answer`, set it only when a person asked for it.
   - `public`: releases the topic on the portal's public page (only when the portal has
     the public view on, and only when a person asked for it).
4. Duplicates: `merge_feedback_topics` (into = the topic that stays, from = the duplicate).
5. Into work: `feedback_to_board` creates a card on a board linked to the topic. Make one
   ticket per team or part (e.g. "Frontend" and "Backend"). When all tickets of a topic are
   done, the topic is done and the reporters are told automatically.
6. External trackers: when the project connected GitHub, Gitea/Forgejo, GitLab or Jira
   (project settings → Feedback portals → External trackers), `forward_feedback_topic`
   opens an issue there (`get_feedback_topic` lists the `trackers` and existing
   `forwards`). The issue gets the summary and report texts, never who reported; later
   reports are added as "+n" comments, and a closed issue counts like a done ticket.
   Forward only when a person asked for it.

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

## AI triage

Portals can sort new reports with the project's background AI (portal setting
`aiTriage`): summary, category and tags from the portal's lists, priority with
a reason (`aiNote`), a suspected duplicate (`duplicateOf`) and a spam flag. The
AI never merges or deletes. When you check a topic, merge real duplicates with
`merge_feedback_topics`, or clear a wrong suggestion with `update_feedback_topic`
`dismiss_duplicate: true`; `spam: false` clears a wrong spam flag.

## Public view

Owners can turn on a portal's **public view** (portal settings): released topics appear
under "Topics" on `/feedback/<handle>` with their state, answer and fixed version. Visitors
vote (one vote per account or browser), follow a topic (account or e-mail; they are told
when its state, answer or fixed version changes) and, if the portal allows it, comment.
Comments from visitors wait for a moderator's approval; team comments are shown at once.
Topics are released one by one by moderators (`update_feedback_topic` `public: true`);
reports themselves and reporters stay private.

## Widget for websites and games

Owners switch on the portal's **widget** (portal settings) and paste one line into their
site or game: `<script src="https://<instance>/feedback-widget.js" data-portal="<handle>" async></script>`.
A button opens the report form in a frame served by Phoedox (only on the websites the
portal lists). People can take a **screenshot** – of the browser tab, or of the game's
canvas (`data-canvas="#game"` or `PhoedoxFeedback.setScreenshot(() => canvas)`) – and
**paint over** it before sending: black boxes are burned into the picture, red boxes and
lines mark the problem. Reports carry the page, viewport and browser, plus what the
product adds with `PhoedoxFeedback.setContext({ level: 3 })`; `setPlayerToken(token)` signs
who reports. Such reports have `via: widget`.

## Routing and follow-up

- Automation rules with the trigger `feedback.topic` (project settings → Automation) turn
  new topics into cards on a board, optionally only for a portal, a category or a report
  type; the card becomes the topic's ticket. Category rules run after the AI triage.
- Cards that are tickets show "+n new reports" when more reports arrive later.
- Moderators get an urgent notification when a topic becomes priority 1; the daily
  digest is the person's notification setting.
- In the moderation view the AI can draft an answer to the reporters; nothing is sent
  until a moderator saves it.
- Portals with "answer known issues automatically" give a new report the answer of a
  matching topic closed in the last 90 days (or of the AI's duplicate); such answers are
  marked `answerAuto`.
- Products can sign who reports and from which build (reporter tokens with the portal's
  player key); such reports carry `player`, `player_name`, `build` and
  `player_verified: yes` in their context.

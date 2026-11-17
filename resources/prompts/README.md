# Prompts

System prompts and instructions for AI clients. Each file `<name>.md` is
embedded at build time and loaded with `resources.Prompt("<name>")`.

To change a prompt without rebuilding, copy the file into a directory and
start Phoedox with `PHOEDOX_PROMPTS_DIR=/path/to/dir`. Files there override the
embedded ones by name.

| File | Used by |
|---|---|
| `mcp-instructions.md` | MCP server instructions sent to every AI client on connect |
| `chat-system.md` | AI chat in projects (ADR-0066): instructions, the same for every chat |
| `chat-context.md` | AI chat: the situation (date, person, project); placeholders `{{…}}` |
| `chat-page.md` | AI chat: the page that is open, added to `chat-context.md` as `{{page}}` |
| `app-ai.md` | Apps asking the AI through the bridge (`phoedox.ai`) |
| `app-comment.md` | Background AI answering "@AI" comments on apps (reply and edits as JSON) |
| `app-comment-failed.md`, `app-comment-suggested.md` | Notes added to that reply when a change failed or waits as a suggestion |

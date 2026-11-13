# Prompts

System prompts and instructions for AI clients. Each file `<name>.md` is
embedded at build time and loaded with `resources.Prompt("<name>")`.

To change a prompt without rebuilding, copy the file into a directory and
start Phoedox with `PHOEDOX_PROMPTS_DIR=/path/to/dir`. Files there override the
embedded ones by name.

| File | Used by |
|---|---|
| `mcp-instructions.md` | MCP server instructions sent to every AI client on connect |

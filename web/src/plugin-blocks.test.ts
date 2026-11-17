import { expect, test } from "bun:test";
import { markdownHTML as renderMarkdownRaw } from "./md";
import { pluginBlocks } from "./plugin-blocks";

test("fenced blocks of a plugin's kind become phx-plugin-block", () => {
  pluginBlocks.set(new Map([["timeline", { kind: "timeline", label: "Timeline", plugin: "com.phoedox.timeline", version: "1.0.0" }]]));
  const html = renderMarkdownRaw("```timeline\nTask: 2026-01-01, 2w\n```\n\n```python\nprint(1)\n```", "haus");
  expect(html).toContain('<phx-plugin-block data-kind="timeline" data-project="haus"><pre>Task: 2026-01-01, 2w</pre></phx-plugin-block>');
  expect(html).toContain('class="language-python"');
  pluginBlocks.set(new Map());
  expect(renderMarkdownRaw("```timeline\nx\n```", "haus")).not.toContain("phx-plugin-block");
});

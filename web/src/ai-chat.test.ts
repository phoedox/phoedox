import { describe, expect, test } from "bun:test";
import { dollars, parseSSE, toolResults, userText, type Message } from "./ai-chat";

describe("AI chat", () => {
  test("parses server-sent events across chunks", () => {
    const a = parseSSE('event: text\ndata: {"type":"text","text":"Hal"}\n\nevent: text\ndata: {"type":"te');
    expect(a.events).toEqual([{ type: "text", text: "Hal" }]);
    const b = parseSSE(a.rest + 'xt","text":"lo"}\n\nevent: done\ndata: {"type":"done","costCents":2}\n\n');
    expect(b.events.map((e) => e.type)).toEqual(["text", "done"]);
    expect(b.events[0].text).toBe("lo");
    expect(b.events[1].costCents).toBe(2);
    expect(b.rest).toBe("");
  });

  test("an error event keeps its key", () => {
    const { events } = parseSSE('event: error\ndata: {"error":"x","code":"invalid","key":"ai_chat.unavailable.user_budget"}\n\n');
    expect(events[0]).toMatchObject({ type: "error", key: "ai_chat.unavailable.user_budget" });
  });

  test("hides notes Phoedox added for the model", () => {
    expect(userText([{ type: "text", text: "[Phoedox: the person created 2 card(s) from proposal t3: “A”]\n\nThanks" }])).toBe("Thanks");
    expect(userText([{ type: "tool_result", id: "x" }])).toBe("");
  });

  test("finds tool results by call id", () => {
    const msgs: Message[] = [
      { seq: 1, role: "assistant", parts: [{ type: "tool_use", id: "t1", name: "edit_page" }], createdAt: "" },
      { seq: 2, role: "user", parts: [{ type: "tool_result", id: "t1", isError: true, text: "forbidden" }], createdAt: "" },
    ];
    expect(toolResults(msgs).get("t1")?.isError).toBe(true);
  });

  test("formats dollars", () => {
    expect(dollars(142, "en")).toBe("$1.42");
  });
});

// AI chat in projects (ADR-0066): types, the event stream of an answer and
// helpers for showing stored messages.
import { ApiError } from "./api";

export interface ChatStatus {
  available: boolean;
  reason?: string;
  model?: string;
  userLimit: number;
  userUsed: number;
  orgLimit: number;
  orgUsed: number;
  orgBudget: number;
  orgSpent: number;
  instanceBlocked?: boolean;
  writeMode: string;
}

export interface ChatModel {
  ref: string;
  providerId: string;
  provider: string;
  model: string;
  label: string;
  own?: boolean;
  price?: { input: number; output: number };
}

export interface ChatModels {
  models: ChatModel[];
  default: string;
}

export interface Chat {
  id: string;
  projectId: string;
  documentId?: string;
  title: string;
  model?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Part {
  type: "text" | "tool_use" | "tool_result";
  text?: string;
  id?: string;
  name?: string;
  input?: Record<string, unknown>;
  isError?: boolean;
}

export interface Message {
  seq: number;
  role: "user" | "assistant";
  parts: Part[];
  createdAt: string;
}

export interface Proposal {
  toolUseId: string;
  status: "accepted" | "rejected";
  result?: { id: string; title: string }[];
}

export interface ChatView extends Chat {
  messages: Message[];
  proposals: Record<string, Proposal>;
  status: ChatStatus;
}

export interface ChatEvent {
  type: "text" | "tool" | "tool_done" | "proposal" | "question" | "notice" | "done" | "error";
  text?: string;
  tool?: string;
  ok?: boolean;
  key?: string;
  costCents?: number;
  // error events carry the API error body
  code?: string;
  params?: Record<string, unknown>;
}

/** Splits a server-sent event stream into events; returns the rest that is
 * not complete yet. */
export function parseSSE(buf: string): { events: ChatEvent[]; rest: string } {
  const events: ChatEvent[] = [];
  const blocks = buf.split("\n\n");
  const rest = blocks.pop() ?? "";
  for (const block of blocks) {
    let name = "message";
    const data: string[] = [];
    for (const line of block.split("\n")) {
      if (line.startsWith("event:")) name = line.slice(6).trim();
      else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
    }
    if (!data.length) continue;
    try {
      const v = JSON.parse(data.join("\n"));
      events.push({ ...v, type: name as ChatEvent["type"] });
    } catch { /* not JSON: ignore */ }
  }
  return { events, rest };
}

/** Sends a message and calls onEvent for each event of the answer. */
export async function sendMessage(project: string, chat: string, body: { text: string; document?: string; language?: string; model?: string; answers?: Record<string, string> },
  onEvent: (e: ChatEvent) => void, signal?: AbortSignal): Promise<void> {
  let res: Response;
  try {
    res = await fetch(`/api/v1/projects/${project}/ai/chats/${chat}/messages`, {
      method: "POST", credentials: "same-origin", signal,
      headers: { "Content-Type": "application/json", "X-Phoedox": "1" }, body: JSON.stringify(body),
    });
  } catch (e) {
    if ((e as Error).name === "AbortError") return;
    throw new ApiError(0, "network", "error.network");
  }
  if (!res.ok || !res.body) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.code ?? "unknown", data.key ?? "error.unknown", data.params ?? {});
  }
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const { events, rest } = parseSSE(buf + dec.decode(value, { stream: true }));
      buf = rest;
      events.forEach(onEvent);
    }
  } catch (e) {
    if ((e as Error).name !== "AbortError") throw e;
  }
}

/** The text a person wrote, without notes Phoedox added for the model. */
export function userText(parts: Part[]): string {
  return parts.filter((p) => p.type === "text").map((p) => p.text ?? "").join("\n")
    .split("\n").filter((l) => !/^\[Phoedox: .*\]$/.test(l)).join("\n").trim();
}

/** Results of tool calls by call id (whether they failed). */
export function toolResults(messages: Message[]): Map<string, Part> {
  const out = new Map<string, Part>();
  for (const m of messages) for (const p of m.parts) if (p.type === "tool_result" && p.id) out.set(p.id, p);
  return out;
}

/** Amount in US dollars from cents, e.g. "$0.42". */
export function dollars(cents: number, locale: string): string {
  return new Intl.NumberFormat(locale, { style: "currency", currency: "USD" }).format(cents / 100);
}

/** Tools that change pages or entries: the page view reloads after them. */
export const WRITE_TOOLS = new Set(["edit_page", "create_document", "create_decision", "update_decision", "upsert_entity", "link_entities", "add_comment"]);

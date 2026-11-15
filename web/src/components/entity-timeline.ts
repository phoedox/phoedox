// Timeline of an entry ("timeline per device"): changes, check results,
// switching actions, agent jobs, reviews and the comments and cards that
// mention it, newest first.

import { LitElement, css, html, nothing } from "lit";
import { api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

interface Item { at: string; kind: string; who?: string; via?: string; title?: string; text?: string; changed?: string[]; ok?: boolean; link?: string }

const icons: Record<string, string> = {
  created: "Plus", changed: "Pencil", deleted: "Trash2", restored: "RotateCcw", merged: "GitMerge", reviewed: "BadgeCheck", meta: "UserRound",
  check_fail: "CircleAlert", check_recover: "CircleCheck", device: "Power", device_grant: "KeyRound", agent_job: "Terminal", agent_result: "Terminal",
  comment: "MessageSquare", task: "SquareKanban", task_done: "SquareCheckBig",
};

export class PhxEntityTimeline extends LitElement {
  static properties = { project: {}, entity: {}, labels: { attribute: false }, items: { state: true }, all: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-top: 22px; }
    h2 { font-size: 17px; margin: 0 0 10px; }
    ol { list-style: none; margin: 0; padding: 0; position: relative; }
    ol::before { content: ""; position: absolute; left: 13px; top: 6px; bottom: 6px; width: 1px; background: var(--line); }
    li { display: grid; grid-template-columns: 28px minmax(0, 1fr); gap: 10px; padding: 6px 0; position: relative; }
    .dot { width: 27px; height: 27px; border-radius: 50%; display: grid; place-items: center; background: var(--surface); border: 1px solid var(--line); color: var(--muted); z-index: 1; }
    .dot.bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 45%, var(--line)); }
    .dot.good { color: var(--good); border-color: color-mix(in srgb, var(--good) 45%, var(--line)); }
    .head { display: flex; flex-wrap: wrap; gap: 4px 8px; align-items: baseline; }
    .head b { font-weight: 500; }
    .when { color: var(--muted); font-size: 12.5px; }
    .via { font-size: 11.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 999px; padding: 0 6px; }
    .text { color: var(--muted); font-size: 13.5px; margin-top: 2px; overflow-wrap: anywhere; }
    code { font-size: 12.5px; }
    .more { margin-top: 6px; }
  `];
  project = "";
  entity = "";
  labels: Record<string, string> = {};
  items?: Item[];
  all = false;

  updated(changed: Map<string, unknown>) {
    if (changed.has("entity") || changed.has("project")) void this.load();
  }

  private async load() {
    if (!this.entity) return;
    this.items = (await api<Item[]>("GET", `/projects/${this.project}/entities/${this.entity}/timeline?limit=200`).catch(() => [])) ?? [];
  }

  private line(it: Item) {
    const k = `timeline.${it.kind}`;
    switch (it.kind) {
      case "changed": return html`${t(k as any)} ${(it.changed ?? []).slice(0, 6).map((f, i) => html`${i ? ", " : ""}${this.labels[f] ? this.labels[f] : html`<code>${f}</code>`}`)}${(it.changed?.length ?? 0) > 6 ? " …" : ""}`;
      case "check_fail": case "check_recover": case "task": case "task_done": case "comment":
        return html`${t(k as any)} ${it.link ? html`<a href=${it.link}>${it.title}</a>` : it.title}`;
      case "device": case "agent_job":
        return html`${t(k as any)} <code>${it.title}</code>${it.ok === false ? html` · <span style="color:var(--bad)">${t("timeline.failed")}</span>` : nothing}`;
      case "agent_result": return t(it.ok === false ? "timeline.agent_failed" : "timeline.agent_result");
      case "device_grant": return t(it.title === "device.ai_control" ? "timeline.device_ai_control" : "timeline.device_grant");
      default: return t(k as any);
    }
  }

  render() {
    const items = this.items;
    if (!items?.length) return nothing;
    const shown = this.all ? items : items.slice(0, 12);
    return html`<h2>${icon("History", 16)} ${t("timeline.title")}</h2>
      <ol>${shown.map((it) => {
        const cls = it.kind === "check_fail" || it.ok === false ? "bad" : it.kind === "check_recover" || it.kind === "task_done" ? "good" : "";
        return html`<li><span class="dot ${cls}">${icon(icons[it.kind] ?? "Circle", 14)}</span>
          <div><div class="head"><b>${this.line(it)}</b></div>
            <div class="head"><span class="when">${formatDate(it.at)}${it.who ? ` · ${it.who}` : ""}</span>${it.via && it.via !== "web" ? html`<span class="via">${t(`timeline.via.${it.via}` as any) || it.via}</span>` : nothing}</div>
            ${it.text ? html`<div class="text">${it.text}</div>` : nothing}</div></li>`;
      })}</ol>
      ${items.length > shown.length ? html`<button class="ghost more" @click=${() => (this.all = true)}>${t("timeline.more", { count: items.length - shown.length })}</button>` : nothing}`;
  }
}
customElements.define("phx-entity-timeline", PhxEntityTimeline);

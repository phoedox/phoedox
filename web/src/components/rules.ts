import { LitElement, css, html } from "lit";
import { ApiError, api, type Doc } from "../api";
import type { MonitorCheck } from "../checks";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { attrLabel, kindName, loadModel, type Model } from "../model";
import { base } from "./shared";
import { toast } from "./toasts";

type TriggerType = "check.fail" | "check.recover" | "entity.create" | "date";
type ActionType = "task" | "notify" | "http";
interface Trigger { type: TriggerType; check?: string; kind?: string; field?: string; daysBefore?: number }
interface Action { type: ActionType; board?: string; column?: string; title?: string; text?: string; labels?: string[]; to?: string; request?: string }
interface Rule {
  id: string; name: string; enabled: boolean; trigger: Trigger; action: Action;
  lastRunAt?: string; lastStatus?: string; lastError?: string;
}

/** Request templates for ticket systems; {{NAME}} come from the project environment. */
const presets: Record<string, string> = {
  zammad: `POST https://zammad.example.com/api/v1/tickets
Authorization: Token token={{ZAMMAD_TOKEN}}
Content-Type: application/json

{"title": {{rule|json}}, "group": "Users", "customer_id": "guess:{{ZAMMAD_CUSTOMER}}",
 "article": {"subject": {{entry|json}}, "body": {{message|json}}, "type": "note", "internal": false}}`,
  jira: `POST https://example.atlassian.net/rest/api/2/issue
Authorization: Basic {{JIRA_BASIC}}
Content-Type: application/json

{"fields": {"project": {"key": "IT"}, "summary": {{rule|json}}, "description": {{message|json}}, "issuetype": {"name": "Task"}}}`,
  github: `POST https://api.github.com/repos/OWNER/REPO/issues
Authorization: Bearer {{GITHUB_TOKEN}}
Accept: application/vnd.github+json
Content-Type: application/json

{"title": {{rule|json}}, "body": {{message|json}}}`,
};

/** Project settings → automation: if this happens, do that (ADR-0060). */
export class PhxRules extends LitElement {
  static properties = {
    project: {}, owner: { type: Boolean }, rules: { state: true }, editing: { state: true }, draft: { state: true },
    model: { state: true }, checks: { state: true }, boards: { state: true }, busy: { state: true }, error: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .intro { margin: 0 0 16px; max-width: 70ch; }
    .list { display: flex; flex-direction: column; gap: 10px; margin-bottom: 16px; }
    .rule { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 12px 14px; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 6px 12px; align-items: center; }
    .rule b { font-size: 15px; }
    .rule .what { font-size: 13.5px; color: var(--muted); display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }
    .rule .what svg { color: var(--accent); }
    .rule .last { font-size: 12.5px; color: var(--muted); grid-column: 1 / -1; }
    .rule .last.bad { color: var(--bad); }
    .rule .btns { display: flex; gap: 6px; align-items: center; }
    .rule.off { opacity: .6; }
    form { display: grid; gap: 12px; border: 1px solid var(--line); border-radius: 12px; padding: 16px; background: var(--surface); max-width: 760px; }
    fieldset { border: 0; padding: 0; margin: 0; display: grid; gap: 10px; }
    legend { font-weight: 700; margin-bottom: 4px; display: flex; gap: 6px; align-items: center; }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    textarea.req { font-family: var(--font-mono); font-size: 13px; min-height: 170px; }
    .vars { font-size: 12.5px; color: var(--muted); }
    .vars code { font-size: 12px; }
    label.check { display: flex; gap: 6px; align-items: center; font-weight: 400; }
    label.check input { width: auto; }
    @media (max-width: 640px) { .two { grid-template-columns: 1fr; } }
  `];
  project = "";
  owner = false;
  rules?: Rule[];
  editing?: string; // rule id or "new"
  draft: { trigger: Trigger; action: Action } = { trigger: { type: "check.fail" }, action: { type: "task" } };
  model?: Model;
  checks: MonitorCheck[] = [];
  boards: Doc[] = [];
  busy = false;
  error = "";

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    [this.rules, this.model, this.checks, this.boards] = await Promise.all([
      api<Rule[]>("GET", `/projects/${this.project}/rules`).catch(() => []),
      loadModel(this.project).catch(() => undefined),
      api<MonitorCheck[]>("GET", `/projects/${this.project}/checks`).catch(() => [] as MonitorCheck[]),
      api<Doc[]>("GET", `/projects/${this.project}/documents?type=board`).then((d) => d.filter((x) => x.type === "board")).catch(() => [] as Doc[]),
    ]);
  }

  private start(r?: Rule) {
    this.error = "";
    this.editing = r?.id ?? "new";
    this.draft = r
      ? { trigger: { ...r.trigger }, action: { ...r.action } }
      : { trigger: { type: "check.fail" }, action: { type: "task", board: this.boards[0]?.slug, title: t("auto.default_title") } };
  }

  /** Date fields of the chosen kind (or of all kinds). */
  private dateFields() {
    const m = this.model;
    if (!m) return [];
    const kinds = this.draft.trigger.kind ? m.kinds.filter((k) => k.id === this.draft.trigger.kind) : m.kinds;
    const seen = new Map<string, string>();
    for (const k of kinds) for (const a of k.attrs) if (a.type === "date" && !seen.has(a.name)) seen.set(a.name, attrLabel(a));
    return [...seen];
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const d = this.draft;
    const trigger: Trigger = { type: d.trigger.type };
    if (d.trigger.type.startsWith("check.")) trigger.check = String(f.get("check") ?? "");
    if (d.trigger.type === "entity.create" || d.trigger.type === "date") trigger.kind = String(f.get("kind") ?? "");
    if (d.trigger.type === "date") {
      trigger.field = String(f.get("field") ?? "");
      trigger.daysBefore = Number(f.get("days") ?? 0);
    }
    const action: Action = { type: d.action.type };
    if (d.action.type === "task") {
      action.board = String(f.get("board") ?? "");
      action.labels = String(f.get("labels") ?? "").split(",").map((x) => x.trim()).filter(Boolean);
    }
    if (d.action.type === "notify") action.to = String(f.get("to") ?? "owners");
    if (d.action.type !== "http") {
      action.title = String(f.get("title") ?? "");
      action.text = String(f.get("text") ?? "");
    } else action.request = String(f.get("request") ?? "");
    const body = { name: String(f.get("name") ?? ""), enabled: f.get("enabled") === "on", trigger, action };
    this.busy = true;
    this.error = "";
    try {
      if (this.editing === "new") await api("POST", `/projects/${this.project}/rules`, body);
      else await api("PUT", `/projects/${this.project}/rules/${this.editing}`, body);
      this.editing = undefined;
      toast(t("auto.saved"), "ok");
      await this.load();
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async toggle(r: Rule) {
    try {
      await api("PUT", `/projects/${this.project}/rules/${r.id}`, { name: r.name, enabled: !r.enabled, trigger: r.trigger, action: r.action });
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async test(r: Rule) {
    try {
      await api("POST", `/projects/${this.project}/rules/${r.id}/test`);
      toast(t("auto.test_ok"), "ok");
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
    await this.load();
  }

  private async removeRule(r: Rule) {
    if (!confirm(t("auto.delete_confirm", { name: r.name }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/rules/${r.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private triggerText(x: Trigger) {
    const kind = x.kind ? kindName(this.model?.kinds.find((k) => k.id === x.kind), x.kind) : t("auto.any_kind");
    switch (x.type) {
      case "check.fail": case "check.recover":
        return t(`auto.when.${x.type === "check.fail" ? "fail" : "recover"}`, { check: this.checks.find((c) => c.id === x.check)?.name ?? t("auto.any_check") });
      case "entity.create": return t("auto.when.create", { kind });
      default: {
        const a = this.model?.kinds.flatMap((k) => k.attrs).find((at) => at.name === x.field);
        return t("auto.when.date", { kind, field: a ? attrLabel(a) : x.field ?? "", days: x.daysBefore ?? 0 });
      }
    }
  }

  private actionText(x: Action) {
    switch (x.type) {
      case "task": return t("auto.then.task", { board: this.boards.find((b) => b.slug === x.board)?.title ?? x.board ?? "" });
      case "notify": return t("auto.then.notify", { to: x.to === "members" ? t("auto.to_members") : x.to === "owners" || !x.to ? t("auto.to_owners") : x.to });
      default: return t("auto.then.http", { host: /https?:\/\/([^/\s]+)/.exec(x.request ?? "")?.[1] ?? "" });
    }
  }

  private form() {
    const d = this.draft;
    const r = this.rules?.find((x) => x.id === this.editing);
    const set = (patch: Partial<Trigger> | Partial<Action>, which: "trigger" | "action") => {
      this.draft = { ...d, [which]: { ...d[which], ...patch } };
    };
    const kinds = this.model?.kinds ?? [];
    return html`<form @submit=${this.save}>
      <label class="field">${t("auto.name")}<input name="name" required maxlength="120" .value=${r?.name ?? ""} placeholder=${t("auto.name_ph")}></label>
      <fieldset><legend>${icon("Zap", 15)} ${t("auto.when")}</legend>
        <select aria-label=${t("auto.when")} @change=${(e: Event) => set({ type: (e.target as HTMLSelectElement).value as TriggerType }, "trigger")}>
          ${(["check.fail", "check.recover", "entity.create", "date"] as TriggerType[]).map((x) => html`<option value=${x} ?selected=${d.trigger.type === x}>${t(`auto.trigger.${x.replace(".", "_")}` as any)}</option>`)}
        </select>
        ${d.trigger.type.startsWith("check.") ? html`<label class="field">${t("auto.check")}<select name="check">
          <option value="">${t("auto.any_check")}</option>
          ${this.checks.map((c) => html`<option value=${c.id} ?selected=${d.trigger.check === c.id}>${c.name}</option>`)}</select></label>` : ""}
        ${d.trigger.type === "entity.create" || d.trigger.type === "date" ? html`<label class="field">${t("auto.kind")}<select name="kind"
            @change=${(e: Event) => set({ kind: (e.target as HTMLSelectElement).value }, "trigger")}>
          <option value="">${t("auto.any_kind")}</option>
          ${kinds.map((k) => html`<option value=${k.id} ?selected=${d.trigger.kind === k.id}>${kindName(k)}</option>`)}</select></label>` : ""}
        ${d.trigger.type === "date" ? html`<div class="two">
          <label class="field">${t("auto.field")}<select name="field" required>
            ${this.dateFields().map(([n, l], _, all) => html`<option value=${n} ?selected=${n === (d.trigger.field ?? ["cancel_by", "warranty_until", "end"].find((f) => all.some(([x]) => x === f)))}>${l}</option>`)}</select></label>
          <label class="field">${t("auto.days")}<input name="days" type="number" min="0" max="365" .value=${String(d.trigger.daysBefore ?? 30)}></label>
        </div>` : ""}
      </fieldset>
      <fieldset><legend>${icon("ArrowRight", 15)} ${t("auto.then")}</legend>
        <select aria-label=${t("auto.then")} @change=${(e: Event) => set({ type: (e.target as HTMLSelectElement).value as ActionType }, "action")}>
          ${(["task", "notify", "http"] as ActionType[]).map((x) => html`<option value=${x} ?selected=${d.action.type === x}>${t(`auto.action.${x}` as any)}</option>`)}
        </select>
        ${d.action.type === "task" ? html`<div class="two">
          <label class="field">${t("auto.board")}<select name="board" required>
            ${this.boards.map((b) => html`<option value=${b.slug} ?selected=${d.action.board === b.slug}>${b.title}</option>`)}</select></label>
          <label class="field">${t("auto.labels")}<input name="labels" .value=${(d.action.labels ?? []).join(", ")} placeholder="monitoring"></label>
        </div>` : ""}
        ${d.action.type === "notify" ? html`<label class="field">${t("auto.to")}<select name="to">
          <option value="owners" ?selected=${!d.action.to || d.action.to === "owners"}>${t("auto.to_owners")}</option>
          <option value="members" ?selected=${d.action.to === "members"}>${t("auto.to_members")}</option></select></label>` : ""}
        ${d.action.type !== "http" ? html`
          <label class="field">${t("auto.title_field")}<input name="title" .value=${d.action.title ?? ""}></label>
          <label class="field">${t("auto.text")}<textarea name="text" rows="3">${d.action.text ?? ""}</textarea></label>`
        : html`
          <div class="row">${t("auto.preset")}:
            ${Object.keys(presets).map((k) => html`<button type="button" class="ghost" @click=${() => set({ request: presets[k] }, "action")}>${t(`auto.preset.${k}` as any)}</button>`)}</div>
          <label class="field">${t("auto.request")}<textarea class="req" name="request" required spellcheck="false" .value=${d.action.request ?? ""}></textarea></label>
          <p class="vars">${t("auto.request_hint")}</p>`}
        <p class="vars">${t("auto.vars")} <code>{{rule}}</code> <code>{{entry}}</code> <code>{{kind}}</code> <code>{{check}}</code> <code>{{status}}</code> <code>{{message}}</code> <code>{{date}}</code> <code>{{days}}</code> <code>{{link}}</code> <code>{{project}}</code></p>
      </fieldset>
      <label class="check"><input type="checkbox" name="enabled" ?checked=${r?.enabled ?? true}> ${t("auto.enabled")}</label>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <div class="row"><button ?disabled=${this.busy}>${t("auto.save")}</button>
        <button type="button" class="ghost" @click=${() => (this.editing = undefined)}>${t("doc.cancel")}</button></div>
    </form>`;
  }

  render() {
    const rules = this.rules;
    if (!rules) return html`<p class="muted">${t("app.loading")}</p>`;
    return html`
      <p class="muted intro">${t("auto.intro")}</p>
      ${rules.length ? html`<div class="list">${rules.map((r) => html`<div class=${r.enabled ? "rule" : "rule off"}>
        <div><b>${r.name}</b>
          <div class="what">${this.triggerText(r.trigger)} ${icon("ArrowRight", 13)} ${this.actionText(r.action)}</div></div>
        ${this.owner ? html`<div class="btns">
          <button class="ghost" title=${r.enabled ? t("auto.disable") : t("auto.enable")} aria-label=${r.enabled ? t("auto.disable") : t("auto.enable")} @click=${() => this.toggle(r)}>${icon(r.enabled ? "Pause" : "Play", 14)}</button>
          <button class="ghost" title=${t("auto.test")} aria-label=${t("auto.test")} @click=${() => this.test(r)}>${icon("FlaskConical", 14)}</button>
          <button class="ghost" title=${t("auto.edit")} aria-label=${t("auto.edit")} @click=${() => this.start(r)}>${icon("Pencil", 14)}</button>
          <button class="ghost" title=${t("auto.delete")} aria-label=${t("auto.delete")} @click=${() => this.removeRule(r)}>${icon("Trash2", 14)}</button></div>` : ""}
        ${r.lastRunAt ? html`<span class=${r.lastStatus === "error" ? "last bad" : "last"}>${t("auto.last", { date: formatDate(r.lastRunAt) })}${r.lastStatus === "error" ? ` – ${r.lastError}` : ""}</span>` : ""}
      </div>`)}</div>` : html`<p class="muted">${t("auto.none")}</p>`}
      ${this.owner ? this.editing ? this.form() : html`<button @click=${() => this.start()}>${icon("Plus", 14)} ${t("auto.new")}</button>` : html`<p class="muted">${t("auto.owner_only")}</p>`}`;
  }
}
customElements.define("phx-rules", PhxRules);

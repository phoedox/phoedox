// <phx-gantt-form .plan=…>: a Mermaid Gantt plan edited in fields – title,
// date and axis format, weekends, sections with their tasks (status,
// critical, milestone, id, start, duration or end). Fires "change" with
// the new plan; the block editor writes it back as Mermaid source.
import { LitElement, css, html, nothing } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { ganttIds, newGanttId, type GanttPlan, type GanttSection, type GanttTag, type GanttTask } from "../mermaid-gantt";
import { base } from "./shared";

const DATE_FORMATS = ["YYYY-MM-DD", "DD.MM.YYYY", "YYYY-MM-DD HH:mm"];
const AXIS_FORMATS: [string, string][] = [["%d.%m", "24.12"], ["%d.%m.%Y", "24.12.2026"], ["%Y-%m-%d", "2026-12-24"], ["%b %d", "Dec 24"], ["KW %V", "KW 52"], ["%b %Y", "Dec 2026"]];

export class PhxGanttForm extends LitElement {
  static properties = { plan: { attribute: false } };
  static styles = [base, css`
    :host { display: block; }
    section { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); margin: 0 0 12px; }
    section > h3 { margin: 0; padding: 10px 14px; font-size: 14px; display: flex; gap: 8px; align-items: center; }
    section > .body { padding: 0 14px 14px; display: grid; gap: 10px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 10px; }
    label { display: grid; gap: 4px; font-size: 12.5px; color: var(--muted); }
    label.check { display: flex; gap: 6px; align-items: center; color: var(--fg); font-size: 13px; }
    label.check input { width: auto; }
    .part { border: 1px solid var(--line); border-radius: 8px; padding: 10px; display: grid; gap: 8px; background: var(--bg); }
    .part header { display: flex; gap: 8px; align-items: center; }
    .part header input { flex: 1; font-weight: 600; }
    .task { display: grid; grid-template-columns: minmax(0, 1fr) auto auto auto auto; gap: 6px 10px; align-items: center; padding: 8px 0; border-top: 1px solid var(--line); }
    .task input, .task select { padding: 5px 8px; min-height: 0; font-size: 13px; }
    .task .name { font-weight: 500; }
    .task select { width: auto; }
    .task .ops { white-space: nowrap; }
    .task .when { grid-column: 1 / -1; display: grid; grid-template-columns: 7em minmax(0, 1fr) 9em; gap: 8px; }
    .task .when label { font-size: 11.5px; }
    @media (max-width: 620px) { .task { grid-template-columns: 1fr 1fr; } .task .when { grid-template-columns: 1fr 1fr 1fr; } }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th { text-align: left; font-weight: 500; color: var(--muted); font-size: 12px; padding: 0 3px 4px; white-space: nowrap; }
    td { padding: 2px 3px; vertical-align: middle; }
    td input, td select { padding: 4px 6px; min-height: 0; font-size: 13px; }
    td.c { text-align: center; width: 1%; }
    td.c input { width: auto; }
    td.ops { width: 1%; white-space: nowrap; }
    td.id input { width: 6.5em; }
    td.dur input { width: 7.5em; }
    button.icon { padding: 3px; border: 0; background: transparent; color: var(--muted); }
    @media (hover: hover) and (pointer: fine) { button.icon:hover { color: var(--fg); } button.icon.del:hover { color: var(--bad); } }
    button.icon:disabled { opacity: .3; }
    .add { justify-self: start; display: inline-flex; gap: 5px; align-items: center; font-size: 13px; padding: 5px 10px; }
    .muted { color: var(--muted); font-size: 12.5px; margin: 0; }
  `];
  plan!: GanttPlan;

  private emit(p: GanttPlan) {
    this.plan = p;
    this.dispatchEvent(new CustomEvent("change", { detail: p }));
  }

  private set(patch: Partial<GanttPlan>) {
    this.emit({ ...this.plan, ...patch });
  }

  private setSections(sections: GanttSection[]) {
    this.set({ sections });
  }

  private setTask(si: number, ti: number, patch: Partial<GanttTask>) {
    this.setSections(this.plan.sections.map((s, i) => (i !== si ? s : { ...s, tasks: s.tasks.map((x, k) => (k === ti ? { ...x, ...patch } : x)) })));
  }

  private moveTask(si: number, ti: number, d: -1 | 1) {
    const tasks = [...this.plan.sections[si].tasks];
    const [x] = tasks.splice(ti, 1);
    tasks.splice(ti + d, 0, x);
    this.setSections(this.plan.sections.map((s, i) => (i === si ? { ...s, tasks } : s)));
  }

  private addTask(si: number) {
    const p = this.plan;
    const all = p.sections.flatMap((s) => s.tasks);
    const prev = [...all].reverse().find((x) => x.id);
    const today = new Date().toISOString().slice(0, 10);
    const task: GanttTask = { name: t("gantt.new_task"), tags: [], id: newGanttId(p), start: prev ? `after ${prev.id}` : today, end: "3d" };
    this.setSections(p.sections.map((s, i) => (i === si ? { ...s, tasks: [...s.tasks, task] } : s)));
  }

  /** Status: open, active or done (exclusive), plus critical and milestone. */
  private tags(task: GanttTask, status: "" | "active" | "done", crit: boolean, milestone: boolean): GanttTag[] {
    const out: GanttTag[] = [];
    if (crit) out.push("crit");
    if (status) out.push(status);
    if (milestone) out.push("milestone");
    return out;
  }

  private general() {
    const p = this.plan;
    const ex = (p.excludes ?? "").split(/\s+/).filter(Boolean);
    const weekends = ex.includes("weekends");
    return html`<section><h3>${icon("Settings2", 15)} ${t("chartf.general")}</h3><div class="body">
      <label>${t("chartf.title")}<input .value=${p.title ?? ""} @change=${(e: Event) => this.set({ title: (e.target as HTMLInputElement).value.trim() || undefined })}></label>
      <div class="grid">
        <label>${t("gantt.date_format")}<select @change=${(e: Event) => this.set({ dateFormat: (e.target as HTMLSelectElement).value })}>
          ${[...new Set([...DATE_FORMATS, p.dateFormat ?? "YYYY-MM-DD"])].map((f) => html`<option value=${f} ?selected=${(p.dateFormat ?? "YYYY-MM-DD") === f}>${f}</option>`)}</select></label>
        <label>${t("gantt.axis_format")}<select @change=${(e: Event) => this.set({ axisFormat: (e.target as HTMLSelectElement).value || undefined })}>
          <option value="" ?selected=${!p.axisFormat}>${t("chartf.auto")}</option>
          ${AXIS_FORMATS.map(([f, ex]) => html`<option value=${f} ?selected=${p.axisFormat === f}>${ex}</option>`)}
          ${p.axisFormat && !AXIS_FORMATS.some(([f]) => f === p.axisFormat) ? html`<option value=${p.axisFormat} selected>${p.axisFormat}</option>` : nothing}</select></label>
      </div>
      <label class="check"><input type="checkbox" .checked=${weekends} @change=${(e: Event) => {
        const on = (e.target as HTMLInputElement).checked;
        const rest = ex.filter((x) => x !== "weekends");
        this.set({ excludes: [...(on ? ["weekends"] : []), ...rest].join(" ") || undefined });
      }}>${t("gantt.weekends")}</label>
      <label class="check"><input type="checkbox" .checked=${p.todayMarker !== "off"} @change=${(e: Event) => this.set({ todayMarker: (e.target as HTMLInputElement).checked ? undefined : "off" })}>${t("gantt.today")}</label>
    </div></section>`;
  }

  private sectionView(s: GanttSection, si: number, ids: string[]) {
    const n = this.plan.sections.length;
    return html`<div class="part">
      <header>
        <input aria-label=${t("gantt.section_name")} placeholder=${t("gantt.section_name")} .value=${s.name} @change=${(e: Event) =>
          this.setSections(this.plan.sections.map((x, i) => (i === si ? { ...x, name: (e.target as HTMLInputElement).value } : x)))}>
        ${n > 1 ? html`<button class="icon del" aria-label=${t("gantt.remove_section")} title=${t("gantt.remove_section")}
          @click=${() => this.setSections(this.plan.sections.filter((_, i) => i !== si))}>${icon("Trash2", 14)}</button>` : nothing}
      </header>
      ${s.tasks.map((task, ti) => {
        const status = task.tags.includes("done") ? "done" : task.tags.includes("active") ? "active" : "";
        const crit = task.tags.includes("crit"), ms = task.tags.includes("milestone");
        const set = (patch: Partial<GanttTask>) => this.setTask(si, ti, patch);
        return html`<div class="task">
          <input class="name" aria-label=${t("gantt.task")} placeholder=${t("gantt.task")} .value=${task.name} @change=${(e: Event) => set({ name: (e.target as HTMLInputElement).value })}>
          <select aria-label=${t("gantt.status")} @change=${(e: Event) => set({ tags: this.tags(task, (e.target as HTMLSelectElement).value as "" | "active" | "done", crit, ms) })}>
            ${(["", "active", "done"] as const).map((v) => html`<option value=${v} ?selected=${status === v}>${t(`gantt.status_${v || "open"}`)}</option>`)}</select>
          <label class="check" title=${t("gantt.crit_hint")}><input type="checkbox" .checked=${crit} @change=${(e: Event) => set({ tags: this.tags(task, status, (e.target as HTMLInputElement).checked, ms) })}>${t("gantt.crit")}</label>
          <label class="check" title=${t("gantt.milestone_hint")}><input type="checkbox" .checked=${ms} @change=${(e: Event) => {
            const on = (e.target as HTMLInputElement).checked;
            set({ tags: this.tags(task, status, crit, on), end: on ? "0d" : task.end === "0d" ? "1d" : task.end });
          }}>${t("gantt.milestone")}</label>
          <span class="ops">
            <button class="icon" aria-label=${t("gantt.up")} title=${t("gantt.up")} ?disabled=${ti === 0} @click=${() => this.moveTask(si, ti, -1)}>${icon("ArrowUp", 13)}</button>
            <button class="icon" aria-label=${t("gantt.down")} title=${t("gantt.down")} ?disabled=${ti === s.tasks.length - 1} @click=${() => this.moveTask(si, ti, 1)}>${icon("ArrowDown", 13)}</button>
            <button class="icon del" aria-label=${t("chartf.remove")} title=${t("chartf.remove")} @click=${() => this.setSections(this.plan.sections.map((x, i) => (i === si ? { ...x, tasks: x.tasks.filter((_, k) => k !== ti) } : x)))}>${icon("X", 14)}</button>
          </span>
          <div class="when">
            <label title=${t("gantt.id_hint")}>${t("gantt.id")}<input placeholder="a1" .value=${task.id ?? ""} @change=${(e: Event) => set({ id: (e.target as HTMLInputElement).value.trim().replace(/[^\w-]/g, "") || undefined })}></label>
            <label>${t("gantt.start")}<input list="gantt-after" placeholder=${t("gantt.start_ph")} .value=${task.start ?? ""} @change=${(e: Event) => set({ start: (e.target as HTMLInputElement).value.trim() || undefined })}></label>
            <label>${t("gantt.duration")}<input placeholder="3d" .value=${task.end} @change=${(e: Event) => set({ end: (e.target as HTMLInputElement).value.trim() })}></label>
          </div>
        </div>`;
      })}
      <button class="ghost add" @click=${() => this.addTask(si)}>${icon("Plus", 13)} ${t("gantt.add_task")}</button>
      <datalist id="gantt-after">${ids.map((id) => html`<option value=${`after ${id}`}></option>`)}</datalist>
    </div>`;
  }

  render() {
    const p = this.plan;
    const ids = ganttIds(p);
    const sections = p.sections.length ? p.sections : [{ name: t("gantt.section_n", { n: 1 }), tasks: [] }];
    return html`${this.general()}
      <section><h3>${icon("ChartGantt", 15)} ${t("gantt.plan")}</h3><div class="body">
        <p class="muted">${t("gantt.hint")}</p>
        ${sections.map((s, si) => this.sectionView(s, si, ids))}
        <button class="ghost add" @click=${() => this.setSections([...sections, { name: t("gantt.section_n", { n: sections.length + 1 }), tasks: [] }])}>${icon("Plus", 13)} ${t("gantt.add_section")}</button>
      </div></section>`;
  }
}
customElements.get("phx-gantt-form") || customElements.define("phx-gantt-form", PhxGanttForm);

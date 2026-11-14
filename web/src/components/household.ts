import { LitElement, css, html } from "lit";
import { ApiError, api, shareURL, type Doc } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown, withoutTitle } from "../md";
import { kindName, loadModel, type Model, findKind } from "../model";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";
import { wifiPayload } from "../wifi";

interface Item { slug: string; name: string; kind: string; price?: number; purchaseDate?: string; warrantyUntil?: string; warrantyDays?: number; kwh: number; cost: number }
interface Household { items: Item[]; value: number; kwh: number; cost: number; pricePerKwh: number; expiring: number }
interface NewLink { token: string; label: string; expiresAt: string | null }

const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** Opens a printable document in a new tab (the content never leaves the browser). */
function printDoc(title: string, body: string) {
  const doc = `<!doctype html><html><head><meta charset="utf-8"><title>${esc(title)}</title><style>
    body{font:15px/1.5 system-ui,sans-serif;max-width:780px;margin:24px auto;padding:0 16px;color:#111}
    h1{font-size:24px} h2{font-size:19px} section{page-break-after:always} section:last-child{page-break-after:auto}
    table{border-collapse:collapse} td,th{border:1px solid #bbb;padding:3px 6px} pre{white-space:pre-wrap;background:#f4f4f4;padding:8px}
    .card{border:2px dashed #999;border-radius:12px;padding:18px;width:320px;text-align:center} .card svg{width:220px;height:220px}
    .muted{color:#555;font-size:13px} @media print{.noprint{display:none}}
  </style></head><body><p class="noprint"><button onclick="print()">${esc(t("house.print"))}</button></p>${body}</body></html>`;
  const url = URL.createObjectURL(new Blob([doc], { type: "text/html" }));
  window.open(url, "_blank", "noopener");
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

/** Household: inventory and warranties, power cost, temporary access, emergency folder, guest Wi-Fi card. */
export class PhxHousehold extends LitElement {
  static properties = { project: {}, h: { state: true }, model: { state: true }, docs: { state: true }, links: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 1050px; margin: 0 auto; padding: 24px 16px 60px; }
    h2 { font-size: 18px; margin: 28px 0 8px; display: flex; gap: 8px; align-items: center; }
    .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 10px; }
    .tile { border: 1px solid var(--line); border-radius: 12px; padding: 10px 14px; background: var(--surface); }
    .tile b { display: block; font-size: 22px; }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th, td { border-bottom: 1px solid var(--line); padding: 5px 8px; text-align: left; }
    td.n, th.n { text-align: right; font-variant-numeric: tabular-nums; }
    .warn { color: var(--bad); font-weight: 600; }
    .soon { color: var(--warn, var(--accent)); font-weight: 600; }
    .pick { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 4px 12px; max-height: 220px; overflow: auto; border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; }
    .pick label { display: flex; gap: 6px; align-items: center; font-weight: 400; }
    .pick input { width: auto; }
    .row select, .row input[type=number] { width: auto; }
    .links { font-size: 13.5px; display: grid; gap: 4px; margin-top: 8px; }
    form.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 8px; align-items: end; }
  `];
  project = "";
  h?: Household;
  model?: Model;
  docs: Doc[] = [];
  links: { title: string; url: string }[] = [];
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    const [h, model, docs] = await Promise.all([
      api<Household>("GET", `/projects/${this.project}/household`).catch(() => undefined),
      loadModel(this.project).catch(() => undefined),
      api<Doc[]>("GET", `/projects/${this.project}/documents`).catch(() => [] as Doc[]),
    ]);
    this.h = h;
    this.model = model;
    this.docs = (docs ?? []).filter((d) => d.type !== "board");
  }

  private get projectRole() {
    return currentProject.get()?.role ?? "";
  }

  private money(v: number) {
    return new Intl.NumberFormat(document.documentElement.lang || undefined, { style: "currency", currency: "EUR" }).format(v);
  }

  private async setPrice(e: Event) {
    const v = Number((e.target as HTMLInputElement).value.replace(",", "."));
    try {
      await api("PUT", `/projects/${this.project}/household/price`, { price: v });
      this.h = await api<Household>("GET", `/projects/${this.project}/household`);
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private checked(form: HTMLFormElement) {
    return [...form.querySelectorAll<HTMLInputElement>("input[name=doc]:checked")].map((i) => i.value);
  }

  private async grantAccess(e: Event) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const slugs = this.checked(form);
    if (!slugs.length) return toast(t("house.pick_pages"), "error");
    this.busy = true;
    const out: { title: string; url: string }[] = [];
    try {
      for (const slug of slugs) {
        const l = await api<NewLink>("POST", `/projects/${this.project}/share-links`, { document: slug, label: String(f.get("label") || t("house.access_label")), days: Number(f.get("days")) });
        out.push({ title: this.docs.find((d) => d.slug === slug)?.title ?? slug, url: shareURL(l.token) });
      }
      this.links = out;
      toast(t("house.access_done", { n: out.length }), "ok");
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async printFolder(e: Event) {
    e.preventDefault();
    const slugs = this.checked(e.target as HTMLFormElement);
    if (!slugs.length) return toast(t("house.pick_pages"), "error");
    const parts: string[] = [];
    for (const slug of slugs) {
      const d = await api<Doc>("GET", `/projects/${this.project}/documents/${slug}`).catch(() => undefined);
      if (!d) continue;
      const c = d.content ?? {};
      const md = d.type === "markdown" ? withoutTitle(c.markdown ?? "", d.title) : d.type === "link" ? `${c.url ?? ""}\n\n${c.description ?? ""}` : c.decision ?? c.description ?? "";
      parts.push(`<section><h1>${esc(d.title)}</h1>${renderMarkdown(md, this.project, d.id)}</section>`);
    }
    const p = currentProject.get();
    printDoc(t("house.folder_title", { project: p?.name ?? this.project }),
      `<section><h1>${esc(t("house.folder_title", { project: p?.name ?? this.project }))}</h1><p class="muted">${esc(t("house.folder_printed", { date: formatDay(new Date().toISOString()) }))}</p><ol>${slugs.map((s) => `<li>${esc(this.docs.find((d) => d.slug === s)?.title ?? s)}</li>`).join("")}</ol></section>${parts.join("")}`);
  }

  private async wifiCard(e: Event) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const ssid = String(f.get("ssid") ?? "").trim(), pass = String(f.get("password") ?? ""), sec = String(f.get("security")), hidden = f.get("hidden") === "on";
    if (!ssid) return;
    try {
      const { svg } = await api<{ svg: string }>("POST", "/qr", { text: wifiPayload(ssid, pass, sec, hidden) });
      printDoc(t("house.wifi_title"), `<div class="card"><h2>${esc(t("house.wifi_title"))}</h2>${svg}<p><b>${esc(t("house.wifi_ssid"))}:</b> ${esc(ssid)}${sec !== "nopass" ? `<br><b>${esc(t("house.wifi_password"))}:</b> ${esc(pass)}` : ""}</p><p class="muted">${esc(t("house.wifi_scan"))}</p></div>`);
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private pagePicker(preselect: (d: Doc) => boolean) {
    return html`<div class="pick">${this.docs.map((d) => html`<label><input type="checkbox" name="doc" value=${d.slug} ?checked=${preselect(d)}> ${d.title}</label>`)}</div>`;
  }

  render() {
    const h = this.h;
    const edit = ["editor", "owner"].includes(this.projectRole);
    const emergency = (d: Doc) => /notfall|emergency|übergabe|handover/i.test(`${d.category ?? ""} ${d.title}`);
    return html`
      <h1>${t("house.title")}</h1>
      ${h ? html`<div class="tiles">
        <div class="tile"><b>${this.money(h.value)}</b>${t("house.value")}</div>
        <div class="tile"><b>${Math.round(h.kwh)} kWh</b>${t("house.kwh_year")}</div>
        <div class="tile"><b>${this.money(h.cost)}</b>${t("house.cost_year")}</div>
        <div class="tile"><b>${h.expiring}</b>${t("house.expiring")}</div>
      </div>
      <p class="row muted" style="margin-top:8px">${t("house.price")}
        ${edit ? html`<input type="number" step="0.01" min="0.01" max="10" .value=${String(h.pricePerKwh)} @change=${this.setPrice} aria-label=${t("house.price")}> €/kWh` : html`<b>${h.pricePerKwh} €/kWh</b>`}</p>
      <h2>${icon("Package", 17)} ${t("house.inventory")}</h2>
      ${h.items.length ? html`<table><thead><tr><th>${t("entity.name")}</th><th>${t("table.kind")}</th><th class="n">${t("attr.price")}</th><th>${t("attr.purchase_date")}</th><th>${t("attr.warranty_until")}</th><th class="n">kWh/${t("house.year")}</th><th class="n">${t("house.cost_short")}</th></tr></thead>
        <tbody>${h.items.map((it) => html`<tr><td><a href="/p/${this.project}/e/${it.slug}">${it.name}</a></td><td>${kindName(findKind(this.model, it.kind), it.kind)}</td>
          <td class="n">${it.price ? this.money(it.price) : ""}</td><td>${it.purchaseDate ? formatDay(it.purchaseDate) : ""}</td>
          <td>${it.warrantyUntil ? html`<span class=${(it.warrantyDays ?? 0) < 0 ? "warn" : (it.warrantyDays ?? 999) <= 60 ? "soon" : ""}>${formatDay(it.warrantyUntil)}${it.warrantyDays !== undefined && it.warrantyDays >= 0 && it.warrantyDays <= 60 ? ` · ${t("house.days_left", { n: it.warrantyDays })}` : ""}${(it.warrantyDays ?? 0) < 0 ? ` · ${t("house.expired")}` : ""}</span>` : ""}</td>
          <td class="n">${it.kwh ? Math.round(it.kwh) : ""}</td><td class="n">${it.cost ? this.money(it.cost) : ""}</td></tr>`)}</tbody></table>`
        : html`<p class="muted">${t("house.empty")}</p>`}` : html`<p class="muted">${t("app.loading")}</p>`}

      ${this.projectRole === "owner" || this.projectRole === "editor" ? html`<h2>${icon("KeyRound", 17)} ${t("house.access")}</h2>
      <p class="muted">${t("house.access_hint")}</p>
      <form @submit=${this.grantAccess}>
        ${this.pagePicker((d) => d.type === "floorplan")}
        <div class="row" style="margin-top:8px"><input name="label" placeholder=${t("house.access_label")} aria-label=${t("house.access_who")}>
          <select name="days" aria-label=${t("house.access_days")}>${[1, 3, 7, 14, 30].map((d) => html`<option value=${d} ?selected=${d === 3}>${t("house.days", { n: d })}</option>`)}</select>
          <button ?disabled=${this.busy}>${icon("Link", 14)} ${t("house.access_create")}</button></div>
      </form>
      ${this.links.length ? html`<div class="links">${this.links.map((l) => html`<span>${l.title}: <a href=${l.url} target="_blank" rel="noopener">${l.url}</a></span>`)}</div>` : ""}` : ""}

      <h2>${icon("BookOpenCheck", 17)} ${t("house.folder")}</h2>
      <p class="muted">${t("house.folder_hint")}</p>
      <form @submit=${this.printFolder}>${this.pagePicker(emergency)}
        <button style="margin-top:8px">${icon("FileText", 14)} ${t("house.folder_print")}</button></form>

      <h2>${icon("Wifi", 17)} ${t("house.wifi_title")}</h2>
      <p class="muted">${t("house.wifi_hint")}</p>
      <form class="grid" @submit=${this.wifiCard}>
        <label>${t("house.wifi_ssid")}<input name="ssid" required autocomplete="off"></label>
        <label>${t("house.wifi_password")}<input name="password" type="password" autocomplete="off"></label>
        <label>${t("house.wifi_security")}<select name="security"><option value="WPA">WPA/WPA2/WPA3</option><option value="WEP">WEP</option><option value="nopass">${t("house.wifi_open")}</option></select></label>
        <label class="row"><input type="checkbox" name="hidden" style="width:auto"> ${t("house.wifi_hidden")}</label>
        <button>${icon("Wifi", 14)} ${t("house.wifi_create")}</button>
      </form>`;
  }
}
customElements.define("phx-household", PhxHousehold);

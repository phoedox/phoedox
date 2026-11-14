import { LitElement, css, html, unsafeCSS } from "lit";
import type * as Leaflet from "leaflet";
import leafletCSS from "leaflet/dist/leaflet.css?inline";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon, iconSVG } from "../icon";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Node { slug: string; name: string; kind: "site" | "building" | "floor" | "room"; attrs?: Record<string, unknown>; devices: number; children: Node[] }
const KIND_ICON = { site: "MapPin", building: "Building2", floor: "Layers", room: "DoorOpen" } as const;
const MAP_OK = "phx.map.tiles";
type Key = Parameters<typeof t>[0];

const coords = (n: Node): [number, number] | undefined => {
  const lat = Number(n.attrs?.lat), lng = Number(n.attrs?.lng);
  return Number.isFinite(lat) && Number.isFinite(lng) && (lat !== 0 || lng !== 0) && n.attrs?.lat !== undefined ? [lat, lng] : undefined;
};

/** Location hierarchy site → building → floor → room with a map of the sites. */
export class PhxLocations extends LitElement {
  static properties = { project: {}, roots: { state: true }, mapOn: { state: true }, placing: { state: true } };
  static styles = [base, unsafeCSS(leafletCSS), css`
    :host { display: block; max-width: 1100px; margin: 0 auto; padding: 24px 16px 60px; }
    ul { list-style: none; margin: 0; padding: 0 0 0 18px; }
    ul.root { padding: 0; }
    li { margin: 3px 0; }
    .node { display: inline-flex; gap: 6px; align-items: center; }
    .count { color: var(--muted); font-size: 13px; }
    #map { height: 380px; border-radius: 12px; border: 1px solid var(--line); margin: 8px 0 20px; z-index: 0; }
    .consent { border: 1px dashed var(--line); border-radius: 12px; padding: 16px; margin: 8px 0 20px; display: grid; gap: 10px; justify-items: start; }
    .pin { color: var(--accent); }
    .placing { font-size: 14px; }
  `];
  project = "";
  roots?: Node[];
  mapOn = false;
  /** Site that gets its position from the next click on the map. */
  placing?: Node;
  private map?: Leaflet.Map;
  private L?: typeof Leaflet;
  private markers?: Leaflet.LayerGroup;

  connectedCallback() {
    super.connectedCallback();
    try {
      this.mapOn = localStorage.getItem(MAP_OK) === "1";
    } catch {
      this.mapOn = false;
    }
    this.load();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.map?.remove();
    this.map = undefined;
  }

  private get canEdit() {
    return ["editor", "owner"].includes(currentProject.get()?.role ?? "");
  }

  private async load() {
    this.roots = (await api<Node[]>("GET", `/projects/${this.project}/locations`).catch(() => [])) ?? [];
    await this.updateComplete;
    if (this.mapOn) this.drawMap();
  }

  private sites(): Node[] {
    return (this.roots ?? []).filter((n) => n.kind === "site");
  }

  private allowMap() {
    try {
      localStorage.setItem(MAP_OK, "1");
    } catch { /* the choice is only remembered when storage works */ }
    this.mapOn = true;
    this.updateComplete.then(() => this.drawMap());
  }

  private async drawMap() {
    const el = this.renderRoot.querySelector<HTMLElement>("#map");
    if (!el) return;
    this.L ??= await import("leaflet");
    const L = this.L;
    if (!this.map) {
      this.map = L.map(el, { zoomControl: true, attributionControl: true });
      L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", { maxZoom: 19, attribution: "© OpenStreetMap" }).addTo(this.map);
      this.map.on("click", (e: Leaflet.LeafletMouseEvent) => this.place(e.latlng));
      this.markers = L.layerGroup().addTo(this.map);
    }
    this.markers!.clearLayers();
    const pinIcon = L.divIcon({ className: "pin", html: iconSVG("MapPin", 28), iconSize: [28, 28], iconAnchor: [14, 28] });
    const pts: [number, number][] = [];
    for (const s of this.sites()) {
      const c = coords(s);
      if (!c) continue;
      pts.push(c);
      L.marker(c, { icon: pinIcon, title: s.name, keyboard: true }).addTo(this.markers!)
        .on("click", () => location.assign(`/p/${this.project}/e/${s.slug}`));
    }
    if (pts.length === 1) this.map.setView(pts[0], 14);
    else if (pts.length) this.map.fitBounds(L.latLngBounds(pts), { padding: [40, 40] });
    else this.map.setView([51, 10], 4);
  }

  private async place(at: Leaflet.LatLng) {
    const site = this.placing;
    if (!site) return;
    this.placing = undefined;
    try {
      const d = await api<{ entity: { kind: string; name: string; slug: string; attrs: Record<string, unknown> } }>("GET", `/projects/${this.project}/entities/${site.slug}`);
      const e = d.entity;
      await api("POST", `/projects/${this.project}/entities`, { kind: e.kind, name: e.name, slug: e.slug,
        attrs: { ...e.attrs, lat: Math.round(at.lat * 1e6) / 1e6, lng: Math.round(at.lng * 1e6) / 1e6 } });
      toast(t("loc.placed", { name: site.name }), "ok");
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private tree(list: Node[], root = false): unknown {
    return html`<ul class=${root ? "root" : ""}>${list.map((n) => html`<li>
      <span class="node">${icon(KIND_ICON[n.kind], 15)} <a href="/p/${this.project}/e/${n.slug}">${n.name}</a>
        <span class="count">${t(`kind.${n.kind}` as Key)}${n.devices ? ` · ${t("loc.entries", { n: n.devices })}` : ""}</span>
        ${n.kind === "site" && this.mapOn && this.canEdit ? html`<button class="ghost icon" title=${t("loc.set_position")} aria-label=${t("loc.set_position_of", { name: n.name })}
          @click=${() => (this.placing = n)}>${icon("Crosshair", 13)}</button>` : ""}</span>
      ${n.children.length ? this.tree(n.children) : ""}</li>`)}</ul>`;
  }

  render() {
    const roots = this.roots;
    return html`
      <h1>${t("loc.title")}</h1>
      <p class="muted">${t("loc.hint")}</p>
      ${this.sites().length ? this.mapOn
        ? html`${this.placing ? html`<p class="placing" role="status">${icon("Crosshair", 14)} ${t("loc.click_map", { name: this.placing.name })}
            <button class="ghost" @click=${() => (this.placing = undefined)}>${t("doc.cancel")}</button></p>` : ""}<div id="map" aria-label=${t("loc.map")}></div>`
        : html`<div class="consent"><span>${t("loc.map_consent")}</span>
            <button class="ghost" @click=${this.allowMap}>${icon("MapPin", 14)} ${t("loc.show_map")}</button></div>` : ""}
      ${roots === undefined ? html`<p class="muted">${t("app.loading")}</p>` : roots.length ? this.tree(roots, true) : html`<p class="muted">${t("loc.none")}</p>`}`;
  }
}
customElements.define("phx-locations", PhxLocations);

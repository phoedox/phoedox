// 3D view of floor plans with three.js (loaded only when someone opens it).
// Walls with doors and windows, room floors in their colours, devices as
// pins on a stem from the floor (kind icon in the colour of the kind's
// group; real device models come later) and racks with their mounted units. One storey is shown at a time and the
// switch between storeys is animated; "All" stacks them. A click on an
// element opens a small info panel with a link to the entry.

import { LitElement, css, html, nothing } from "lit";
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { CSS2DObject, CSS2DRenderer } from "three/addons/renderers/CSS2DRenderer.js";
import type { Edge, Entity } from "../api";
import type { Floorplan } from "../floorplan";
import { t } from "../i18n";
import { icon, iconSVG } from "../icon";
import { attrLabel, findKind, formatValue, groupColor, kindName, type Model } from "../model";
import { CUT_HEIGHT, buildScene, mountedByRack, stackLevels, type EntryInfo, type Level, type Scene3D } from "../plan3d-model";
import { navigate } from "../state";
import { base } from "./shared";

export interface PlanInput { plan: Floorplan; floor?: string; title?: string; own?: boolean }

/** How far a storey travels while it fades in or out (metres). */
const TRAVEL = 3.2;
const SWITCH_MS = 700;
const reducedMotion = () => matchMedia("(prefers-reduced-motion: reduce)").matches;
const easeInOut = (x: number) => (x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2);

export class PhxPlan3D extends LitElement {
  static properties = {
    project: {}, plans: { attribute: false }, entities: { attribute: false }, edges: { attribute: false }, model: { attribute: false },
    cut: { type: Boolean }, error: { state: true }, selected: { state: true }, active: { state: true }, stacked: { state: true }, copied: { state: true },
  };
  static styles = [base, css`
    :host { display: block; position: relative; }
    .view { position: relative; width: 100%; height: min(70vh, 640px); border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: var(--surface); touch-action: none; }
    canvas { display: block; }
    .labels { position: absolute; inset: 0; pointer-events: none; }
    .lbl { font: 600 12px var(--font-body); color: var(--muted); background: color-mix(in srgb, var(--surface) 80%, transparent); padding: 1px 6px; border-radius: 6px; white-space: nowrap; }
    .lbl.level { color: var(--accent); }
    .lbl.link { pointer-events: auto; cursor: pointer; }
    @media (hover: hover) and (pointer: fine) { .lbl.link:hover { color: var(--accent); } }

    /* Pins: a round badge with the kind's icon; the name shows on hover and
       while selected. */
    .pinw { position: relative; pointer-events: auto; }
    .pin { width: 22px; height: 22px; padding: 0; border-radius: 50%; display: grid; place-items: center; background: var(--c); color: var(--surface);
      border: 1.6px solid var(--surface); box-shadow: 0 1px 4px rgb(0 0 0 / 0.25); cursor: pointer; transition: transform var(--dur-fast) var(--ease-out); }
    .pin .ico { width: 13px; height: 13px; stroke-width: 2.5; }
    .pin:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .pinw.sel .pin { outline: 2px solid var(--accent); outline-offset: 2px; }
    .pn { position: absolute; left: calc(100% + 6px); top: 50%; transform: translateY(-50%); font: 600 11px var(--font-body); color: var(--fg); white-space: nowrap;
      background: color-mix(in srgb, var(--surface) 90%, transparent); border: 1px solid var(--line); padding: 1px 6px; border-radius: 6px; opacity: 0; pointer-events: none;
      transition: opacity var(--dur-fast) var(--ease-out); }
    .pinw.sel .pn, .pinw:focus-within .pn { opacity: 1; }
    @media (hover: hover) and (pointer: fine) { .pinw:hover .pn { opacity: 1; } .pin:hover { transform: scale(1.12); } }
    @media (prefers-reduced-motion: reduce) { .pin { transition: none; } }

    .hint { position: absolute; left: 10px; bottom: 8px; font-size: 12px; color: var(--muted); pointer-events: none; }
    .floors { position: absolute; left: 10px; top: 10px; display: flex; gap: 2px; padding: 3px; background: var(--surface); border: 1px solid var(--line); border-radius: 9px; box-shadow: 0 4px 14px -8px rgb(0 0 0 / 0.3); }
    .floors button { height: 28px; padding: 0 10px; border: 0; border-radius: 6px; background: transparent; color: var(--muted); font: 500 13px var(--font-body); cursor: pointer; }
    .floors button[aria-pressed="true"] { background: var(--accent-soft); color: var(--accent); }
    @media (hover: hover) and (pointer: fine) { .floors button:hover { color: var(--fg); } }
    .ftag { position: absolute; right: 12px; top: 12px; text-align: right; pointer-events: none; font: 600 14px var(--font-display); color: var(--fg); }
    .ftag small { display: block; font: 400 12px var(--font-body); color: var(--muted); }

    /* Info panel of the selected element. */
    .info { position: absolute; right: 12px; bottom: 12px; width: min(300px, calc(100% - 24px)); max-height: calc(100% - 24px); overflow: auto; display: flex; flex-direction: column; gap: 10px;
      padding: 12px 14px; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 12px 32px -14px rgb(0 0 0 / 0.35);
      animation: rise var(--dur) var(--ease-out); }
    @keyframes rise { from { opacity: 0; transform: translateY(6px) scale(0.98); } }
    @media (prefers-reduced-motion: reduce) { .info { animation: fade var(--dur) ease; } @keyframes fade { from { opacity: 0; } } }
    .info header { display: flex; gap: 10px; align-items: flex-start; }
    .info header .badge { flex: none; width: 30px; height: 30px; border-radius: 50%; display: grid; place-items: center; background: var(--c); color: var(--surface); }
    .info header > div { flex: 1; min-width: 0; }
    .info .k { font-size: 12px; color: var(--muted); }
    .info h4 { margin: 0; font: 600 15px var(--font-display); overflow-wrap: anywhere; }
    .info .x { flex: none; width: 28px; height: 28px; padding: 0; display: grid; place-items: center; }
    .info dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 3px 12px; margin: 0; font-size: 13px; }
    .info dt { color: var(--muted); }
    .info dd { margin: 0; overflow-wrap: anywhere; }
    .info .ip { display: inline-flex; align-items: center; gap: 4px; font-family: var(--font-mono); }
    .info .ip button { width: 24px; height: 24px; padding: 0; display: grid; place-items: center; }
    .info ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; font-size: 13px; }
    .info ul button { width: 100%; display: flex; justify-content: space-between; gap: 8px; text-align: left; padding: 4px 6px; border-radius: 6px; background: var(--surface2); border: 0; color: var(--fg); cursor: pointer; }
    .info ul button span:last-child { color: var(--muted); font-family: var(--font-mono); font-size: 12px; }
    .info .go { align-self: flex-start; display: inline-flex; align-items: center; gap: 6px; }
  `];

  project = "";
  plans: PlanInput[] = [];
  entities: Entity[] = [];
  edges: Edge[] = [];
  model?: Model;
  /** Cut the shown storey's walls at 1.2 m to look inside. */
  cut = false;
  error = "";
  selected = "";
  /** Index of the shown storey (in stacked order, lowest first). */
  active = 0;
  /** Show all storeys stacked instead of one. */
  stacked = false;
  copied = false;
  private framed = false;
  private renderer?: THREE.WebGLRenderer;
  private labels?: CSS2DRenderer;
  private scene?: THREE.Scene;
  private camera?: THREE.PerspectiveCamera;
  private controls?: OrbitControls;
  private resize?: ResizeObserver;
  private pickable: THREE.Object3D[] = [];
  private bySlug = new Map<string, THREE.Mesh[]>();
  private pinEls = new Map<string, HTMLElement>();
  private groups: THREE.Group[] = [];
  private levels: Level[] = [];
  private data?: Scene3D;
  private hovered = "";
  private down?: [number, number];
  private anim = 0;
  /** The plan of the shown storey, so a rebuild keeps showing it. */
  private activePlan?: Floorplan;

  connectedCallback() {
    super.connectedCallback();
    // On the window: focus may sit outside the view (or on a button that
    // the panel just removed) and Esc should still close the panel.
    window.addEventListener("keydown", this.onKey);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("keydown", this.onKey);
    cancelAnimationFrame(this.anim);
    this.dispose();
  }

  updated(changed: Map<string, unknown>) {
    if (changed.has("plans") || changed.has("entities") || changed.has("edges") || changed.has("cut") || changed.has("model")) this.build();
    if (changed.has("selected")) this.highlight();
  }

  private onKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape" || !this.selected || e.defaultPrevented) return;
    const target = e.composedPath()[0] as HTMLElement | undefined;
    if (target?.closest?.("input, textarea, select, [contenteditable=true]")) return;
    this.selected = "";
  };

  private get view() {
    return this.renderRoot.querySelector<HTMLElement>(".view");
  }

  private token(name: string, fallback = "#888") {
    return getComputedStyle(this).getPropertyValue(name).trim() || fallback;
  }

  private setup(view: HTMLElement) {
    try {
      this.renderer = new THREE.WebGLRenderer({ antialias: true });
    } catch {
      this.error = t("plan3d.no_webgl");
      return false;
    }
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    view.prepend(this.renderer.domElement);
    this.labels = new CSS2DRenderer();
    this.labels.domElement.className = "labels";
    view.append(this.labels.domElement);
    this.camera = new THREE.PerspectiveCamera(45, 1, 0.05, 2000);
    this.controls = new OrbitControls(this.camera, this.renderer.domElement);
    this.controls.maxPolarAngle = Math.PI / 2 - 0.02; // never below the floor
    this.controls.addEventListener("change", () => this.draw());
    this.resize = new ResizeObserver(() => this.fitSize());
    this.resize.observe(view);
    const el = this.renderer.domElement;
    el.addEventListener("pointerdown", (e) => (this.down = [e.clientX, e.clientY]));
    el.addEventListener("pointerup", (e) => {
      // A click (not a drag) selects the element under the pointer, or
      // closes the info panel on empty space.
      if (!this.down || Math.hypot(e.clientX - this.down[0], e.clientY - this.down[1]) > 4) return;
      this.selected = this.pick(e) ?? "";
    });
    el.addEventListener("pointermove", (e) => this.hover(this.pick(e) ?? ""));
    el.addEventListener("pointerleave", () => this.hover(""));
    return true;
  }

  private fitSize() {
    const view = this.view;
    if (!view || !this.renderer || !this.camera) return;
    const { width, height } = view.getBoundingClientRect();
    this.renderer.setSize(width, height);
    this.labels!.setSize(width, height);
    this.camera.aspect = width / Math.max(height, 1);
    this.camera.updateProjectionMatrix();
    this.draw();
  }

  private draw() {
    if (this.renderer && this.scene && this.camera) {
      this.renderer.render(this.scene, this.camera);
      this.labels!.render(this.scene, this.camera);
    }
  }

  private pick(e: PointerEvent): string | undefined {
    const r = this.renderer!.domElement.getBoundingClientRect();
    const ray = new THREE.Raycaster();
    ray.setFromCamera(new THREE.Vector2(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1), this.camera!);
    const visible = this.pickable.filter((o) => o.parent?.visible !== false);
    return ray.intersectObjects(visible, false).find((h) => h.object.userData.slug)?.object.userData.slug as string | undefined;
  }

  /** Lights all parts of an element: hovered a little, selected more. */
  private glow(slug: string) {
    const on = slug === this.selected ? 0.45 : slug === this.hovered ? 0.3 : 0;
    for (const m of this.bySlug.get(slug) ?? []) {
      const mat = m.material as THREE.MeshStandardMaterial;
      mat.emissive?.set(on ? this.token("--accent") : 0x000000);
      mat.emissiveIntensity = on;
    }
  }

  private hover(slug: string) {
    if (slug === this.hovered) return;
    const old = this.hovered;
    this.hovered = slug;
    this.glow(old);
    this.glow(slug);
    if (this.renderer) this.renderer.domElement.style.cursor = slug ? "pointer" : "";
    this.draw();
  }

  private lastSelected = "";
  private highlight() {
    const old = this.lastSelected;
    this.lastSelected = this.selected;
    this.glow(old);
    this.glow(this.selected);
    this.pinEls.get(old)?.classList.remove("sel");
    this.pinEls.get(this.selected)?.classList.add("sel");
    this.draw();
  }

  private build() {
    const view = this.view;
    if (!view) return;
    if (!this.renderer && !this.setup(view)) return;
    cancelAnimationFrame(this.anim);
    this.clear();
    const entries = new Map<string, EntryInfo>(this.entities.map((e) => [e.slug, { slug: e.slug, name: e.name, kind: e.kind, attrs: e.attrs ?? {} }]));
    const levels = stackLevels(this.plans.map((p) => ({ plan: p.plan, floor: p.floor ? entries.get(p.floor) : undefined })));
    // Keep showing the same storey; at first the one of this page.
    const own = this.plans.find((p) => p.own)?.plan;
    const keep = levels.findIndex((l) => l.plan === (this.activePlan ?? own));
    this.active = Math.max(0, keep >= 0 ? keep : levels.findIndex((l) => l.plan === own));
    this.activePlan = levels[this.active]?.plan;
    if (this.cut && levels.length) levels[this.stacked ? levels.length - 1 : this.active].cut = CUT_HEIGHT;
    this.levels = levels;
    this.data = buildScene(levels, entries, mountedByRack(this.edges));
    this.scene = this.makeScene(this.data);
    this.place();
    // Keep the camera when only the cut or the data changes.
    if (!this.framed) this.frame();
    this.framed = true;
    this.highlight();
    this.fitSize();
    // The storey bar and tag depend on the storeys found here.
    this.requestUpdate();
  }

  /** Shows the active storey on the ground (or all stacked). */
  private place() {
    this.groups.forEach((g, i) => {
      const show = this.stacked || i === this.active;
      g.visible = show;
      g.position.y = this.stacked ? 0 : -this.levels[i].elevation / 1000;
      g.traverse((o) => { if (o.userData.levelName) o.visible = this.stacked; });
      this.fade(g, 1);
    });
  }

  /** Fades a storey's materials and labels (o = 0…1). */
  private fade(g: THREE.Group, o: number) {
    g.traverse((x) => {
      if (x instanceof THREE.Mesh || x instanceof THREE.LineSegments) {
        for (const mat of Array.isArray(x.material) ? x.material : [x.material]) {
          const baseOpacity = (mat.userData.opacity as number | undefined) ?? 1;
          mat.opacity = baseOpacity * o;
          mat.transparent = baseOpacity < 1 || o < 1;
          mat.depthWrite = baseOpacity >= 1 && o >= 1;
        }
      }
      if (x instanceof CSS2DObject) {
        x.element.style.opacity = o < 1 ? String(o) : "";
        x.element.style.pointerEvents = o < 1 ? "none" : "";
      }
    });
  }

  /** Switches to another storey: the old one moves away and fades out, the
   * new one comes from the other side (up = the old one sinks). */
  private showLevel(i: number) {
    if (i === this.active && !this.stacked) return;
    cancelAnimationFrame(this.anim);
    const from = this.active, wasStacked = this.stacked;
    this.active = i;
    this.activePlan = this.levels[i]?.plan;
    this.stacked = false;
    if (this.selected && !this.levelOf(this.selected).includes(i)) this.selected = "";
    if (this.cut) {
      // The cut belongs to the shown storey.
      this.build();
      return;
    }
    if (wasStacked || reducedMotion() || from === i) {
      this.place();
      this.draw();
      return;
    }
    const dir = i > from ? 1 : -1;
    const a = this.groups[from], b = this.groups[i];
    const ya = -this.levels[from].elevation / 1000, yb = -this.levels[i].elevation / 1000;
    a.visible = b.visible = true;
    const start = performance.now();
    const step = (now: number) => {
      const p = Math.min(1, (now - start) / SWITCH_MS), e = easeInOut(p);
      a.position.y = ya - dir * TRAVEL * e;
      b.position.y = yb + dir * TRAVEL * (1 - e);
      this.fade(a, 1 - e);
      this.fade(b, e);
      this.draw();
      if (p < 1) this.anim = requestAnimationFrame(step);
      else {
        this.place();
        this.draw();
      }
    };
    this.anim = requestAnimationFrame(step);
  }

  private showAll() {
    if (this.stacked) return;
    cancelAnimationFrame(this.anim);
    this.stacked = true;
    if (this.cut) this.build();
    else this.place();
    this.frame();
    this.draw();
  }

  /** Storeys an element appears on. */
  private levelOf(slug: string): number[] {
    const d = this.data;
    if (!d) return [];
    return [...new Set([...d.pins, ...d.boxes, ...d.slabs].filter((x) => x.slug === slug).map((x) => x.level))];
  }

  private makeScene(data: Scene3D): THREE.Scene {
    const scene = new THREE.Scene();
    scene.background = new THREE.Color(this.token("--surface", "#fff"));
    scene.add(new THREE.HemisphereLight(0xffffff, new THREE.Color(this.token("--surface2", "#eee")), 2.2));
    const sun = new THREE.DirectionalLight(0xffffff, 1.6);
    sun.position.set(data.max[0] + 8, data.max[1] + 15, data.max[2] + 10);
    scene.add(sun);
    const mat = (color: string, extra: THREE.MeshStandardMaterialParameters = {}) => {
      const m = new THREE.MeshStandardMaterial({ color: new THREE.Color(color), roughness: 0.85, ...extra });
      m.userData.opacity = extra.opacity ?? 1;
      return m;
    };
    // Each storey has its own materials, so it can fade on its own.
    this.groups = this.levels.map(() => new THREE.Group());
    this.groups.forEach((g) => scene.add(g));
    const group = (level: number) => this.groups[level] ?? this.groups[0];
    const palettes = this.levels.map(() => ({
      wall: mat(this.token("--surface2", "#eee")),
      glass: mat(this.token("--wire", "#36c"), { transparent: true, opacity: 0.28, roughness: 0.1 }),
      rack: mat(this.token("--fg", "#222")),
      unit: mat(this.token("--muted", "#666")),
      edge: new THREE.LineBasicMaterial({ color: new THREE.Color(this.token("--muted", "#666")) }),
      stem: new THREE.LineBasicMaterial({ color: new THREE.Color(this.token("--muted", "#666")) }),
      dot: mat(this.token("--accent", "#c50"), { transparent: true, opacity: 0.55 }),
    }));
    this.pickable = [];
    this.bySlug.clear();
    this.pinEls.clear();
    const pickable = (mesh: THREE.Mesh, slug?: string) => {
      mesh.userData.slug = slug;
      if (!slug) return;
      this.pickable.push(mesh);
      this.bySlug.set(slug, [...(this.bySlug.get(slug) ?? []), mesh]);
    };
    for (const b of data.boxes) {
      const pal = palettes[b.level] ?? palettes[0];
      // Pickable elements get their own material so hover can light them alone.
      const m = b.slug ? pal[b.kind].clone() : pal[b.kind];
      if (b.slug) m.userData.opacity = pal[b.kind].userData.opacity;
      const geo = new THREE.BoxGeometry(b.w, b.h, b.d);
      const mesh = new THREE.Mesh(geo, m);
      mesh.position.set(b.x, b.y, b.z);
      mesh.rotation.y = b.rot;
      if (b.kind === "wall" || b.kind === "rack") mesh.add(new THREE.LineSegments(new THREE.EdgesGeometry(geo), pal.edge));
      group(b.level).add(mesh);
      pickable(mesh, b.slug);
    }
    for (const s of data.slabs) {
      const shape = new THREE.Shape(s.points.map(([x, z]) => new THREE.Vector2(x, -z)));
      const geo = new THREE.ShapeGeometry(shape);
      geo.rotateX(-Math.PI / 2);
      const mesh = new THREE.Mesh(geo, mat(this.token(`--tb-${s.color}`, this.token("--surface2")), { side: THREE.DoubleSide }));
      mesh.position.y = s.y;
      group(s.level).add(mesh);
      pickable(mesh, s.slug);
    }
    // Cables as thin tubes along their route.
    const cableColor: Record<string, string> = { network: "--wire", power: "--bad", coax: "--signal", other: "--muted" };
    for (const c of data.cables) {
      const path = new THREE.CurvePath<THREE.Vector3>();
      for (let i = 1; i < c.points.length; i++) {
        path.add(new THREE.LineCurve3(new THREE.Vector3(...c.points[i - 1]), new THREE.Vector3(...c.points[i])));
      }
      const geo = new THREE.TubeGeometry(path, Math.max(8, c.points.length * 8), 0.03, 8, false);
      const mesh = new THREE.Mesh(geo, mat(this.token(cableColor[c.kind] ?? "--muted"), { roughness: 0.5 }));
      group(c.level).add(mesh);
      pickable(mesh, c.slug);
    }
    for (const l of data.labels) {
      const div = document.createElement("div");
      div.className = `lbl ${l.kind}${l.slug ? " link" : ""}`;
      div.textContent = l.text;
      if (l.slug) div.addEventListener("click", () => (this.selected = l.slug!));
      const o = new CSS2DObject(div);
      o.position.set(l.x, l.y, l.z);
      // Storey names only help while the storeys are stacked.
      o.userData.levelName = l.kind === "level";
      group(l.level).add(o);
    }
    for (const p of data.pins) {
      const k = findKind(this.model, p.kind);
      // A thin stem from the floor and a dot where the device stands.
      const pal = palettes[p.level] ?? palettes[0];
      if (p.y - 0.14 > p.foot) {
        const stem = new THREE.Line(new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(p.x, p.foot, p.z), new THREE.Vector3(p.x, p.y - 0.14, p.z)]), pal.stem);
        group(p.level).add(stem);
      }
      const dot = new THREE.Mesh(new THREE.CircleGeometry(0.07, 20), pal.dot);
      dot.rotation.x = -Math.PI / 2;
      dot.position.set(p.x, p.foot + 0.004, p.z);
      group(p.level).add(dot);
      const wrap = document.createElement("div");
      wrap.className = "pinw";
      const btn = document.createElement("button");
      btn.className = "pin";
      btn.style.setProperty("--c", groupColor[k?.group ?? ""] ?? "var(--accent)");
      btn.setAttribute("aria-label", `${p.name} · ${kindName(k, p.kind)}`);
      btn.innerHTML = iconSVG(k?.icon ?? "Cpu", 13);
      btn.addEventListener("click", () => (this.selected = this.selected === p.slug ? "" : p.slug));
      btn.addEventListener("pointerenter", () => this.hover(p.slug));
      btn.addEventListener("pointerleave", () => this.hover(""));
      const name = document.createElement("span");
      name.className = "pn";
      name.textContent = p.name;
      wrap.append(btn, name);
      this.pinEls.set(p.slug, wrap);
      const o = new CSS2DObject(wrap);
      o.position.set(p.x, p.y, p.z);
      group(p.level).add(o);
    }
    return scene;
  }

  /** Looks at the shown storeys from the south-east, a bit from above. */
  private frame() {
    const box = new THREE.Box3();
    this.groups.forEach((g) => g.visible && box.expandByObject(g));
    if (box.isEmpty() && this.data) box.set(new THREE.Vector3(...this.data.min), new THREE.Vector3(...this.data.max));
    const c = box.getCenter(new THREE.Vector3()), s = box.getSize(new THREE.Vector3());
    const size = Math.max(s.x, s.y, s.z, 2);
    this.camera!.position.set(c.x + size * 0.55, c.y + size * 0.7, c.z + size * 0.8);
    this.controls!.target.set(c.x, Math.min(c.y, 1), c.z);
    this.controls!.maxDistance = size * 6;
    this.controls!.update();
  }

  private clear() {
    this.scene?.traverse((o) => {
      if (o instanceof THREE.Mesh || o instanceof THREE.LineSegments) {
        o.geometry.dispose();
        (Array.isArray(o.material) ? o.material : [o.material]).forEach((m) => m.dispose());
      }
      if (o instanceof CSS2DObject) o.element.remove();
    });
    this.scene = undefined;
    this.groups = [];
    this.hovered = "";
  }

  private dispose() {
    this.clear();
    this.resize?.disconnect();
    this.controls?.dispose();
    this.renderer?.dispose();
    this.renderer?.domElement.remove();
    this.labels?.domElement.remove();
    this.renderer = undefined;
  }

  resetView() {
    this.framed = false;
    this.build();
  }

  private levelName(i: number) {
    const lv = this.levels[i];
    const input = this.plans.find((p) => p.plan === lv?.plan);
    return lv?.name || input?.title || t("plan3d.storey", { n: i + 1 });
  }

  private floorBar() {
    if (this.levels.length < 2) return nothing;
    return html`<div class="floors" role="group" aria-label=${t("plan3d.storeys")}>
      ${this.levels.map((_, i) => html`<button aria-pressed=${!this.stacked && i === this.active ? "true" : "false"} @click=${() => this.showLevel(i)}>${this.levelName(i)}</button>`)}
      <button aria-pressed=${this.stacked ? "true" : "false"} @click=${() => this.showAll()}>${t("plan3d.all")}</button>
    </div>`;
  }

  private floorTag() {
    const lv = this.levels[this.active];
    if (this.stacked || !lv || this.levels.length < 2) return nothing;
    return html`<div class="ftag" aria-live="polite">${this.levelName(this.active)}
      <small>${t("plan3d.rooms", { count: lv.plan.rooms.length })} · ${t("plan3d.devices", { count: lv.plan.devices.length })}</small></div>`;
  }

  private follow(e: MouseEvent, slug: string) {
    if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    navigate(`/p/${this.project}/e/${slug}`);
  }

  private async copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      this.copied = true;
      setTimeout(() => (this.copied = false), 1200);
    } catch { /* clipboard not allowed: nothing to show */ }
  }

  private infoPanel() {
    const slug = this.selected;
    if (!slug) return nothing;
    const e = this.entities.find((x) => x.slug === slug);
    const name = e?.name ?? this.data?.pins.find((p) => p.slug === slug)?.name ?? slug;
    const kindId = e?.kind ?? "";
    const k = findKind(this.model, kindId);
    const room = this.edges.find((x) => x.rel === "located_in" && x.fromSlug === slug && x.toKind === "room")?.toName;
    const attrs = k?.attrs ?? [];
    const ipAttr = attrs.find((a) => a.type === "ip" && e?.attrs[a.name]);
    const rows = attrs.filter((a) => a !== ipAttr && formatValue(a, e?.attrs[a.name])).slice(0, 6);
    const area = this.data?.slabs.filter((s) => s.slug === slug).reduce((sum, s) => sum + s.area, 0) ?? 0;
    const units = (mountedByRack(this.edges).get(slug) ?? []).sort((a, b) => (b.u ?? 0) - (a.u ?? 0));
    const go = kindId === "room" ? "plan3d.open_room" : kindId === "floor" || !kindId ? "plan3d.open_entry" : "plan3d.open_device";
    const loc = document.documentElement.lang || "en";
    return html`<aside class="info" role="dialog" aria-label=${name}>
      <header>
        <span class="badge" style="--c:${groupColor[k?.group ?? ""] ?? "var(--accent)"}">${icon(k?.icon ?? "Cpu", 16)}</span>
        <div><div class="k">${kindId ? kindName(k, kindId) : ""}${room ? ` · ${room}` : ""}</div><h4>${name}</h4></div>
        <button class="ghost x" aria-label=${t("plan3d.close")} title=${t("plan3d.close")} @click=${() => (this.selected = "")}>${icon("X", 15)}</button>
      </header>
      ${ipAttr || rows.length || area ? html`<dl>
        ${ipAttr ? html`<dt>${attrLabel(ipAttr)}</dt><dd><span class="ip">${String(e!.attrs[ipAttr.name])}
          <button class="ghost" aria-label=${t("plan3d.copy_ip")} title=${t("plan3d.copy_ip")} @click=${() => this.copy(String(e!.attrs[ipAttr.name]))}>${icon(this.copied ? "Check" : "Copy", 13)}</button></span></dd>` : nothing}
        ${rows.map((a) => html`<dt>${attrLabel(a)}</dt><dd>${formatValue(a, e?.attrs[a.name])}</dd>`)}
        ${area ? html`<dt>${t("plan.area_head")}</dt><dd>${area.toLocaleString(loc, { maximumFractionDigits: 1 })} m²</dd>` : nothing}
      </dl>` : nothing}
      ${units.length ? html`<div><div class="k">${t("plan3d.mounted")}</div><ul>${units.map((u) => html`<li><button @click=${() => (this.selected = u.slug)}>
        <span>${u.name}</span><span>${u.u ? t("plan3d.unit_pos", { u: u.u }) : ""}</span></button></li>`)}</ul></div>` : nothing}
      <a class="btn go" href="/p/${this.project}/e/${slug}" @click=${(ev: MouseEvent) => this.follow(ev, slug)}>${t(go)} ${icon("ArrowRight", 14)}</a>
    </aside>`;
  }

  render() {
    return html`${this.error ? html`<p class="muted">${this.error}</p>` : ""}
      <div class="view" role="img" aria-label=${t("plan3d.label")}><span class="hint">${t("plan3d.hint")}</span></div>
      ${this.floorBar()}${this.floorTag()}${this.infoPanel()}`;
  }
}
customElements.define("phx-plan3d", PhxPlan3D);

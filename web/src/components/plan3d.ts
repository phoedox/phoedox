// 3D view of floor plans with three.js (loaded only when someone opens it).
// Walls with doors and windows, room floors in their colours, devices and
// racks with their mounted units; click an element to open its entry.

import { LitElement, css, html } from "lit";
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { CSS2DObject, CSS2DRenderer } from "three/addons/renderers/CSS2DRenderer.js";
import type { Edge, Entity } from "../api";
import type { Floorplan } from "../floorplan";
import { t } from "../i18n";
import { CUT_HEIGHT, buildScene, mountedByRack, stackLevels, type EntryInfo, type Scene3D } from "../plan3d-model";
import { navigate } from "../state";
import { base } from "./shared";

export interface PlanInput { plan: Floorplan; floor?: string }

export class PhxPlan3D extends LitElement {
  static properties = { project: {}, plans: { attribute: false }, entities: { attribute: false }, edges: { attribute: false }, cut: { type: Boolean }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; position: relative; }
    .view { position: relative; width: 100%; height: min(70vh, 640px); border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: var(--surface); touch-action: none; }
    canvas { display: block; }
    .labels { position: absolute; inset: 0; pointer-events: none; }
    .lbl { font: 600 12px var(--font-body); color: var(--fg); background: color-mix(in srgb, var(--surface) 85%, transparent); padding: 1px 6px; border-radius: 6px; white-space: nowrap; }
    .lbl.device { font-weight: 500; font-size: 11px; }
    .lbl.level { color: var(--accent); }
    .lbl.link { pointer-events: auto; cursor: pointer; }
    @media (hover: hover) and (pointer: fine) { .lbl.link:hover { color: var(--accent); } }
    .hint { position: absolute; left: 10px; bottom: 8px; font-size: 12px; color: var(--muted); pointer-events: none; }
  `];

  project = "";
  plans: PlanInput[] = [];
  entities: Entity[] = [];
  edges: Edge[] = [];
  /** Cut the top storey's walls at 1.2 m to look inside. */
  cut = false;
  error = "";
  private framed = false;
  private renderer?: THREE.WebGLRenderer;
  private labels?: CSS2DRenderer;
  private scene?: THREE.Scene;
  private camera?: THREE.PerspectiveCamera;
  private controls?: OrbitControls;
  private resize?: ResizeObserver;
  private pickable: THREE.Object3D[] = [];
  private hovered?: THREE.Mesh;
  private down?: [number, number];

  disconnectedCallback() {
    super.disconnectedCallback();
    this.dispose();
  }

  willUpdate(changed: Map<string, unknown>) {
    // Another set of storeys: look at it anew.
    if (changed.has("plans") && (changed.get("plans") as PlanInput[] | undefined)?.length !== this.plans.length) this.framed = false;
  }

  updated(changed: Map<string, unknown>) {
    if (changed.has("plans") || changed.has("entities") || changed.has("edges") || changed.has("cut")) this.build();
  }

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
      // A click (not a drag) opens the entry under the pointer.
      if (!this.down || Math.hypot(e.clientX - this.down[0], e.clientY - this.down[1]) > 4) return;
      const slug = this.pick(e)?.userData.slug as string | undefined;
      if (slug) navigate(`/p/${this.project}/e/${slug}`);
    });
    el.addEventListener("pointermove", (e) => this.hover(e));
    el.addEventListener("pointerleave", () => this.hover());
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

  private pick(e: PointerEvent): THREE.Object3D | undefined {
    const r = this.renderer!.domElement.getBoundingClientRect();
    const ray = new THREE.Raycaster();
    ray.setFromCamera(new THREE.Vector2(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1), this.camera!);
    return ray.intersectObjects(this.pickable, false).find((h) => h.object.userData.slug)?.object;
  }

  private hover(e?: PointerEvent) {
    const hit = e ? (this.pick(e) as THREE.Mesh | undefined) : undefined;
    if (hit === this.hovered) return;
    const glow = (m: THREE.Mesh | undefined, on: boolean) => {
      const mat = m?.material as THREE.MeshStandardMaterial | undefined;
      mat?.emissive?.set(on ? this.token("--accent") : 0x000000);
      if (mat) mat.emissiveIntensity = on ? 0.35 : 0;
    };
    glow(this.hovered, false);
    glow(hit, true);
    this.hovered = hit;
    this.renderer!.domElement.style.cursor = hit ? "pointer" : "";
    this.draw();
  }

  private build() {
    const view = this.view;
    if (!view) return;
    if (!this.renderer && !this.setup(view)) return;
    this.clear();
    const entries = new Map<string, EntryInfo>(this.entities.map((e) => [e.slug, { slug: e.slug, name: e.name, kind: e.kind, attrs: e.attrs ?? {} }]));
    const levels = stackLevels(this.plans.map((p) => ({ plan: p.plan, floor: p.floor ? entries.get(p.floor) : undefined })));
    if (this.cut && levels.length) levels[levels.length - 1].cut = CUT_HEIGHT;
    const data = buildScene(levels, entries, mountedByRack(this.edges));
    this.scene = this.makeScene(data);
    // Keep the camera when only the cut changes.
    if (!this.framed) this.frame(data);
    this.framed = true;
    this.fitSize();
  }

  private makeScene(data: Scene3D): THREE.Scene {
    const scene = new THREE.Scene();
    scene.background = new THREE.Color(this.token("--surface", "#fff"));
    scene.add(new THREE.HemisphereLight(0xffffff, new THREE.Color(this.token("--surface2", "#eee")), 2.2));
    const sun = new THREE.DirectionalLight(0xffffff, 1.6);
    sun.position.set(data.max[0] + 8, data.max[1] + 15, data.max[2] + 10);
    scene.add(sun);
    const mat = (color: string, extra: THREE.MeshStandardMaterialParameters = {}) => new THREE.MeshStandardMaterial({ color: new THREE.Color(color), roughness: 0.85, ...extra });
    const materials = {
      wall: mat(this.token("--surface2", "#eee")),
      glass: mat(this.token("--wire", "#36c"), { transparent: true, opacity: 0.28, roughness: 0.1 }),
      rack: mat(this.token("--fg", "#222")),
      unit: mat(this.token("--muted", "#666")),
      device: mat(this.token("--accent", "#c50")),
    };
    const edgeMat = new THREE.LineBasicMaterial({ color: new THREE.Color(this.token("--muted", "#666")) });
    this.pickable = [];
    for (const b of data.boxes) {
      // Pickable elements get their own material so hover can light them alone.
      const m = b.slug ? materials[b.kind].clone() : materials[b.kind];
      const geo = new THREE.BoxGeometry(b.w, b.h, b.d);
      const mesh = new THREE.Mesh(geo, m);
      mesh.position.set(b.x, b.y, b.z);
      mesh.rotation.y = b.rot;
      mesh.userData.slug = b.slug;
      if (b.kind === "wall" || b.kind === "rack") mesh.add(new THREE.LineSegments(new THREE.EdgesGeometry(geo), edgeMat));
      scene.add(mesh);
      if (b.slug) this.pickable.push(mesh);
    }
    for (const s of data.slabs) {
      const shape = new THREE.Shape(s.points.map(([x, z]) => new THREE.Vector2(x, -z)));
      const geo = new THREE.ShapeGeometry(shape);
      geo.rotateX(-Math.PI / 2);
      const mesh = new THREE.Mesh(geo, mat(this.token(`--tb-${s.color}`, this.token("--surface2")), { side: THREE.DoubleSide }));
      mesh.position.y = s.y;
      mesh.userData.slug = s.slug;
      scene.add(mesh);
      if (s.slug) this.pickable.push(mesh);
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
      mesh.userData.slug = c.slug;
      scene.add(mesh);
      if (c.slug) this.pickable.push(mesh);
    }
    for (const l of data.labels) {
      const div = document.createElement("div");
      div.className = `lbl ${l.kind}${l.slug ? " link" : ""}`;
      div.textContent = l.text;
      if (l.slug) div.addEventListener("click", () => navigate(`/p/${this.project}/e/${l.slug}`));
      const o = new CSS2DObject(div);
      o.position.set(l.x, l.y, l.z);
      scene.add(o);
    }
    return scene;
  }

  /** Looks at the whole building from the south-east, a bit from above. */
  private frame(data: Scene3D) {
    const [cx, cy, cz] = [0, 1, 2].map((i) => (data.min[i] + data.max[i]) / 2);
    const size = Math.max(data.max[0] - data.min[0], data.max[2] - data.min[2], data.max[1] - data.min[1], 2);
    this.camera!.position.set(cx + size * 0.55, cy + size * 0.7, cz + size * 0.8);
    this.controls!.target.set(cx, Math.min(cy, 1), cz);
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
    this.hovered = undefined;
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

  render() {
    return html`${this.error ? html`<p class="muted">${this.error}</p>` : ""}
      <div class="view" role="img" aria-label=${t("plan3d.label")}><span class="hint">${t("plan3d.hint")}</span></div>`;
  }
}
customElements.define("phx-plan3d", PhxPlan3D);

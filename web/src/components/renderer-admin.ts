// Server-side browser (ADR-0095): system admins switch on the headless
// Chrome service of the Helm chart / docker-compose and choose what it is
// used for (screens, apps, boards, PDF export).

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { scard, settingsUI, srow, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface View {
  enabled: boolean;
  off?: string[];
  uses: string[];
  target?: string;
  status: { configured: boolean; reachable: boolean; browser?: string; error?: string };
}

export class PhxRendererAdmin extends LitElement {
  static properties = { v: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .body { padding: 0 18px 16px; display: grid; gap: 8px; }
    .muted { color: var(--muted); font-size: 13px; margin: 0; }
    .state { display: inline-flex; gap: 6px; align-items: center; font-size: 13px; }
    .ok { color: var(--good); } .bad { color: var(--bad); }
    code { font: 12.5px var(--font-mono); }
  `];
  v?: View;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.v = await api<View>("GET", "/admin/renderer").catch(() => undefined);
  }

  private async save(patch: { enabled?: boolean; off?: string[] }) {
    const v = this.v!;
    this.busy = true;
    try {
      this.v = await api<View>("PUT", "/admin/renderer", { enabled: patch.enabled ?? v.enabled, off: patch.off ?? v.off ?? [] });
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const v = this.v;
    if (!v) return nothing;
    const st = v.status;
    const off = new Set(v.off ?? []);
    const state = !st.configured
      ? html`<span class="state bad">${icon("CircleAlert", 14)} ${t("renderer.not_configured")}</span>`
      : st.reachable
        ? html`<span class="state ok">${icon("CircleCheck", 14)} ${t("renderer.reachable", { browser: st.browser ?? "" })}</span>`
        : html`<span class="state bad">${icon("CircleAlert", 14)} ${t("renderer.unreachable", { msg: st.error ?? "" })}</span>`;
    return scard(t("renderer.title"), t("renderer.intro"), html`
      ${srow(t("renderer.status"), v.target ? t("renderer.target", { url: v.target }) : "", state)}
      ${srow(t("renderer.enabled"), t("renderer.enabled_help"), sswitch(v.enabled, t("renderer.enabled"), (on) => void this.save({ enabled: on }), this.busy || (!v.enabled && !st.reachable)))}
      ${v.enabled ? v.uses.map((u) => srow(t(`renderer.use_${u}` as Key), t(`renderer.use_${u}_help` as Key),
        sswitch(!off.has(u), t(`renderer.use_${u}` as Key), (on) => void this.save({ off: on ? [...off].filter((x) => x !== u) : [...off, u] }), this.busy))) : nothing}
      ${!st.configured ? html`<div class="body"><p class="muted">${t("renderer.setup")}</p><p class="muted"><code>renderer.enabled: true</code> · <code>PHOEDOX_RENDERER_URL</code></p></div>` : nothing}`);
  }
}
customElements.get("phx-renderer-admin") || customElements.define("phx-renderer-admin", PhxRendererAdmin);

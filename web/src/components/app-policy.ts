// App switches (apps-definition): the instance and organizations can switch
// apps off, cap the bridge level, forbid AI calls and allow network hosts.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { scard, settingsUI, srow, sseg, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Policy { off?: boolean; maxLevel?: number | null; noAi?: boolean; noPublic?: boolean; noWebEmbeds?: boolean; embedSites?: string[]; hosts?: string[]; orgHosts?: boolean; instanceOrgHosts?: boolean }

export class PhxAppPolicy extends LitElement {
  static properties = { scope: {}, org: {}, p: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    textarea { width: 100%; min-height: 70px; font: 13px var(--font-mono); box-sizing: border-box; }
    .body { padding: 0 18px 16px; display: grid; gap: 8px; }
    .hint { color: var(--muted); font-size: 13px; margin: 0; }
  `];
  scope: "instance" | "org" = "instance";
  org = "";
  p?: Policy;
  busy = false;
  private loadedFor = "";

  willUpdate() {
    const key = `${this.scope}:${this.org}`;
    if (key !== this.loadedFor) {
      this.loadedFor = key;
      void api<Policy>("GET", this.path()).then((p) => (this.p = p)).catch(() => undefined);
    }
  }

  private path() {
    return this.scope === "org" ? `/orgs/${this.org}/apps` : "/admin/apps";
  }

  private async save(next: Policy) {
    this.busy = true;
    try {
      const { instanceOrgHosts, ...body } = next;
      this.p = { ...(await api<Policy>("PUT", this.path(), body)), instanceOrgHosts };
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const p = this.p;
    if (!p) return nothing;
    const lvl = p.maxLevel === undefined || p.maxLevel === null ? "any" : String(p.maxLevel);
    const hostsAllowed = this.scope === "instance" || p.instanceOrgHosts;
    return html`${scard(t("apppol.title"), t(this.scope === "org" ? "apppol.text_org" : "apppol.text_instance"), html`
      ${srow(t("apppol.off"), t("apppol.off_help"), sswitch(!!p.off, t("apppol.off"), (on) => this.save({ ...p, off: on }), this.busy))}
      ${srow(t("apppol.level"), t("apppol.level_help"), sseg<string>(t("apppol.level"), lvl,
        [["any", t("apppol.level_any")], ["0", "0"], ["1", "1"], ["2", "2"]], (v) => this.save({ ...p, maxLevel: v === "any" ? null : Number(v) })))}
      ${srow(t("apppol.no_ai"), t("apppol.no_ai_help"), sswitch(!!p.noAi, t("apppol.no_ai"), (on) => this.save({ ...p, noAi: on }), this.busy))}
      ${srow(t("apppol.no_public"), t("apppol.no_public_help"), sswitch(!!p.noPublic, t("apppol.no_public"), (on) => this.save({ ...p, noPublic: on }), this.busy))}
      ${this.scope === "instance" ? srow(t("apppol.org_hosts"), t("apppol.org_hosts_help"), sswitch(!!p.orgHosts, t("apppol.org_hosts"), (on) => this.save({ ...p, orgHosts: on }), this.busy)) : nothing}
      <div class="body">
        <b>${t("apppol.hosts")}</b>
        ${hostsAllowed ? html`<p class="hint">${t("apppol.hosts_help")}</p>
          <textarea aria-label=${t("apppol.hosts")} .value=${(p.hosts ?? []).join("\n")} placeholder="api.example.com"
            @change=${(e: Event) => this.save({ ...p, hosts: (e.target as HTMLTextAreaElement).value.split(/[\s,]+/).filter(Boolean) })}></textarea>`
          : html`<p class="hint">${t("apppol.hosts_instance_only")}</p>`}
      </div>`)}
    ${scard(t("embedpol.title"), t(this.scope === "org" ? "embedpol.text_org" : "embedpol.text_instance"), html`
      ${srow(t("embedpol.off"), t("embedpol.off_help"), sswitch(!!p.noWebEmbeds, t("embedpol.off"), (on) => this.save({ ...p, noWebEmbeds: on }), this.busy))}
      <div class="body">
        <b>${t("embedpol.sites")}</b>
        <p class="hint">${t("embedpol.sites_help")}</p>
        <textarea aria-label=${t("embedpol.sites")} .value=${(p.embedSites ?? []).join("\n")} placeholder="figma.com" ?disabled=${!!p.noWebEmbeds}
          @change=${(e: Event) => this.save({ ...p, embedSites: (e.target as HTMLTextAreaElement).value.split(/[\s,]+/).filter(Boolean) })}></textarea>
      </div>`)}`;
  }
}
customElements.get("phx-app-policy") || customElements.define("phx-app-policy", PhxAppPolicy);

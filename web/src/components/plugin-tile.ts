// The tile of a plugin (ADR-0090, concept "Plugin logos and screenshots"):
// the product's own logo where the plugin ships one, otherwise the manifest's
// Lucide icon on a rounded square in the colour of its category.
import { css, html } from "lit";
import { icon, lucideName } from "../icon";
import { t } from "../i18n";

export interface TileSource {
  name: string;
  icon?: string;
  category?: string;
  logo?: string;
  logoDark?: string;
}

/** Category → colour token (other and unknown use the accent). */
export const categoryColor: Record<string, string> = {
  home: "var(--tc-orange)", network: "var(--tc-blue)", servers: "var(--tc-purple)", cloud: "var(--tc-pink)",
  monitoring: "var(--tc-green)", development: "var(--tc-gray)", records: "var(--tc-red)", blocks: "var(--tc-yellow)",
};

/** Label of a category for people. */
export function categoryLabel(c?: string): string {
  return t(`plugins.cat.${c && c in categoryColor ? c : "other"}` as never);
}

/** The tile, size in pixels (24, 38/42, 72 …). */
export function pluginTile(p: TileSource, size = 38) {
  const style = `--tile:${size}px;--c:${categoryColor[p.category ?? ""] ?? "var(--accent)"}`;
  if (p.logo) {
    return html`<span class="ptile logo" style=${style} aria-hidden="true">
      <img class=${p.logoDark ? "lt" : ""} src=${p.logo} alt="" loading="lazy" decoding="async" />
      ${p.logoDark ? html`<img class="dk" src=${p.logoDark} alt="" loading="lazy" decoding="async" />` : ""}</span>`;
  }
  return html`<span class="ptile" style=${style} aria-hidden="true">${icon(p.icon ? lucideName(p.icon) : "Puzzle", Math.round(size * 0.55))}</span>`;
}

/** Styles for pluginTile; add them to a component's styles. */
export const tileStyles = css`
  .ptile { flex: none; width: var(--tile); height: var(--tile); border-radius: calc(var(--tile) * 0.25); display: inline-grid; place-items: center;
    color: var(--c); background: color-mix(in srgb, var(--c) 13%, var(--surface)); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--c) 22%, transparent); }
  .ptile.logo { background: var(--logo-plate, transparent); box-shadow: inset 0 0 0 1px var(--line); overflow: hidden; }
  .ptile.logo:not(:has(.dk)) { background: var(--logo-plate, transparent); }
  .ptile.logo:has(.dk) { background: var(--surface); }
  .ptile img { width: 72%; height: 72%; object-fit: contain; }
  .ptile img.lt { display: var(--light-only, inline-block); }
  .ptile img.dk { display: var(--dark-only, none); }
`;

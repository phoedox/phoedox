// A colour field for diagram forms: default, the palette (tokens) or a
// custom hex colour via the browser's colour picker.
import { css, html, nothing, type TemplateResult } from "lit";
import { t, type Key } from "../i18n";
import { PALETTE, colorCss, colorHex, isHex, isPalette } from "../mermaid-forms/colors";

export const colorFieldCss = css`
  .cf { display: flex; gap: 6px; align-items: center; }
  .cf .sw { width: 18px; height: 18px; border-radius: 5px; border: 1px solid var(--line); flex: none; }
  .cf .sw.none { background: repeating-linear-gradient(135deg, transparent 0 4px, var(--line) 4px 5px); }
  .cf select { flex: 1; min-width: 7em; }
  .cf input[type="color"] { width: 30px; height: 26px; padding: 1px; flex: none; border-radius: 6px; }
`;

/** value: "", a palette name or a hex colour; change gets the new value ("" = default). */
export function colorField(label: string, value: unknown, change: (v: string) => void, size = "s"): TemplateResult {
  const v = typeof value === "string" ? value : "";
  const custom = isHex(v);
  const sw = v && (isPalette(v) || custom) ? colorCss(v) : null;
  return html`<label class="${size} colorf">${label}<span class="cf">
    <span class="sw ${sw ? "" : "none"}" style=${sw ? `background:${sw.fill};border-color:${sw.stroke}` : ""}></span>
    <select @change=${(e: Event) => {
      const x = (e.target as HTMLSelectElement).value;
      change(x === "custom" ? (custom ? v : colorHex(isPalette(v) ? v : "blue", "stroke")) : x);
    }}>
      <option value="" ?selected=${!v}>${t("color.none")}</option>
      ${PALETTE.map((c) => html`<option value=${c} ?selected=${v === c}>${t(`color.${c}` as Key)}</option>`)}
      <option value="custom" ?selected=${custom}>${t("wb.custom_color")}</option>
    </select>
    ${custom ? html`<input type="color" aria-label=${t("wb.custom_color")} .value=${v.length === 4 ? `#${v[1]}${v[1]}${v[2]}${v[2]}${v[3]}${v[3]}` : v}
      @change=${(e: Event) => change((e.target as HTMLInputElement).value.toLowerCase())}>` : nothing}
  </span></label>`;
}

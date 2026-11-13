import { svg, type SVGTemplateResult } from "lit";
import { unsafeSVG } from "lit/directives/unsafe-svg.js";
import * as lucide from "lucide";

type Node = [string, Record<string, string>][];

/** Renders a Lucide icon (names in PascalCase, e.g. "ExternalLink"). */
export function icon(name: string, size = 16): SVGTemplateResult {
  const node = (lucide as unknown as Record<string, Node>)[name];
  const inner = (node ?? []).map(([tag, attrs]) => `<${tag} ${Object.entries(attrs).map(([k, v]) => `${k}="${v}"`).join(" ")}/>`).join("");
  return svg`<svg class="ico" width=${size} height=${size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${unsafeSVG(inner)}</svg>`;
}

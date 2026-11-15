// Building blocks of the settings pages (ADR-0070): cards of setting rows
// (label and explanation left, control right), segmented choices and
// switches. Used by personal settings, administration and project settings.

import { css, html, nothing, type TemplateResult } from "lit";

export const settingsUI = css`
  .page-head { display: grid; gap: 4px; margin-bottom: 18px; }
  .page-head h1 { font-size: 28px; }
  .page-head p { margin: 0; color: var(--muted); max-width: 62ch; }
  .scard { background: var(--surface); border: 1px solid var(--line); border-radius: 12px; margin-bottom: 18px; }
  .scard > header { display: flex; flex-wrap: wrap; gap: 2px 12px; align-items: baseline; justify-content: space-between; padding: 14px 18px 10px; }
  .scard > header h2 { font: 600 15px var(--font-body); margin: 0; }
  .scard > header p { margin: 0; color: var(--muted); font-size: 13px; }
  .scard > .body { padding: 0 18px 16px; }
  .scard.danger { border-color: color-mix(in srgb, var(--bad) 40%, var(--line)); }
  .scard.danger > header h2 { color: var(--bad); }
  .srow { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 320px); gap: 8px 24px; align-items: center; padding: 14px 18px; border-top: 1px solid var(--line); }
  .srow.wide { grid-template-columns: 1fr; }
  .srow > .l b { display: block; font-weight: 500; }
  .srow > .l span { display: block; color: var(--muted); font-size: 13px; }
  .srow > .c { display: flex; gap: 8px; justify-content: flex-end; align-items: center; flex-wrap: wrap; min-width: 0; }
  .srow > .c > input:not([type="checkbox"]):not([type="radio"]), .srow > .c > select { flex: 1; }
  .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
  .seg button { height: 32px; padding: 0 11px; border: 0; border-radius: 0; background: var(--surface); color: var(--muted); font-weight: 500; }
  .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
  .seg button:active { transform: none; }
  .sw { position: relative; width: 40px; height: 22px; padding: 0; border-radius: 999px; background: var(--line); border: 0; flex: none; transition: background var(--dur-fast) var(--ease-out); }
  .sw::after { content: ""; position: absolute; top: 3px; left: 3px; width: 16px; height: 16px; border-radius: 50%; background: var(--surface); transition: transform var(--dur-fast) var(--ease-out); }
  .sw[aria-checked="true"] { background: var(--accent); }
  .sw[aria-checked="true"]::after { transform: translateX(18px); }
  .sw:active { transform: none; }
  .slist > .item { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 12px; align-items: center; padding: 12px 18px; border-top: 1px solid var(--line); }
  .slist .item .t b { display: block; font-weight: 500; overflow-wrap: anywhere; }
  .slist .item .t span { color: var(--muted); font-size: 13px; }
  .slist .bx { width: 34px; height: 34px; border-radius: 9px; display: grid; place-items: center; background: var(--surface2); color: var(--muted); }
  .choices { display: grid; gap: 8px; padding: 0 18px 16px; }
  .choice { display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; cursor: pointer; color: var(--fg); font-size: 14px; }
  .choice input { grid-row: span 2; margin-top: 3px; width: auto; accent-color: var(--accent); }
  .choice b { font-weight: 500; } .choice span { color: var(--muted); font-size: 13px; }
  .choice:has(input:checked) { border-color: var(--accent); background: color-mix(in srgb, var(--accent-soft) 45%, transparent); }
  .swatches { display: flex; gap: 6px; flex-wrap: wrap; }
  .swatches button { width: 26px; height: 26px; padding: 0; border-radius: 7px; border: 2px solid transparent; }
  .swatches button[aria-pressed="true"] { border-color: var(--fg); }
  @media (prefers-reduced-motion: reduce) { .sw, .sw::after { transition: none; } }
  @media (max-width: 760px) {
    .srow { grid-template-columns: 1fr; }
    .srow > .c { justify-content: flex-start; }
  }
`;

/** A page title with an explanation. */
export const pageHead = (title: string, text?: string) =>
  html`<div class="page-head"><h1>${title}</h1>${text ? html`<p>${text}</p>` : nothing}</div>`;

/** A card of setting rows. */
export const scard = (title: string, sub: string | undefined, body: unknown, cls = "") =>
  html`<section class="scard ${cls}"><header><h2>${title}</h2>${sub ? html`<p>${sub}</p>` : nothing}</header>${body}</section>`;

/** One setting: label and explanation left, control right. */
export const srow = (label: string, help: string | undefined, control: unknown, id?: string): TemplateResult =>
  html`<div class="srow" id=${id ?? nothing}><div class="l"><b>${label}</b>${help ? html`<span>${help}</span>` : nothing}</div><div class="c">${control}</div></div>`;

/** A switch that saves at once. */
export const sswitch = (on: boolean, label: string, change: (on: boolean) => void, disabled = false) =>
  html`<button type="button" class="sw" role="switch" aria-checked=${on ? "true" : "false"} aria-label=${label} ?disabled=${disabled}
    @click=${() => change(!on)}></button>`;

/** Segmented choice. */
export function sseg<T extends string>(label: string, value: T, options: [T, string][], change: (v: T) => void) {
  return html`<div class="seg" role="group" aria-label=${label}>${options.map(([v, text]) =>
    html`<button type="button" aria-pressed=${v === value ? "true" : "false"} @click=${() => v !== value && change(v)}>${text}</button>`)}</div>`;
}

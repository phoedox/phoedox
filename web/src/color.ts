// Project colours: derive light/dark variants with >= 4.5:1 contrast.
const hx = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
const toHex = (c: number[]) => "#" + c.map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, "0")).join("");
const lum = (c: number[]) =>
  c.map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; })
   .reduce((a, v, i) => a + v * [0.2126, 0.7152, 0.0722][i], 0);
export const contrast = (a: number[], b: number[]) => {
  const x = lum(a), y = lum(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
};

export function projectColors(hex: string): { light: string; dark: string } {
  let l = hx(hex), d = hx(hex);
  for (let i = 0; i < 40 && contrast(l, [255, 255, 255]) < 4.6; i++) l = l.map((v) => v * 0.94);
  for (let i = 0; i < 40 && contrast(d, [28, 24, 21]) < 4.6; i++) d = d.map((v) => v + (255 - v) * 0.08);
  return { light: toHex(l), dark: toHex(d) };
}

/** Applies a project colour to an element via --pl/--pd (see tokens.css). */
export function applyProjectColor(el: HTMLElement, hex: string) {
  const c = projectColors(hex);
  el.style.setProperty("--pl", c.light);
  el.style.setProperty("--pd", c.dark);
  el.dataset.pc = hex;
}

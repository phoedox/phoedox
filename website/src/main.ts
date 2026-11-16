// The product website: self-hosted fonts and Lucide icons (no CDN), the
// knowledge-graph demo in the hero, copy buttons, and links to operator
// pages (legal texts, contact) that disappear while those are not set.
import "@fontsource-variable/bricolage-grotesque/opsz.css";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import {
  ArrowRight, Bot, Check, Copy, Download, History, Lock, LogIn, MapPin, Server, ShieldCheck, Sparkles, createElement, type IconNode,
} from "lucide";

const icons: Record<string, IconNode> = { ArrowRight, Bot, Check, Copy, Download, History, Lock, LogIn, MapPin, Server, ShieldCheck, Sparkles };

for (const el of document.querySelectorAll<HTMLElement>("i[data-ic]")) {
  const node = icons[el.dataset.ic ?? ""];
  if (!node) continue;
  const svg = createElement(node);
  svg.setAttribute("width", "16");
  svg.setAttribute("height", "16");
  svg.setAttribute("aria-hidden", "true");
  svg.classList.add("ico");
  for (const c of el.classList) svg.classList.add(c);
  el.replaceWith(svg);
}

// Operator pages: hide the link while the server has no text for it.
const checked = new Map<string, Promise<boolean>>();
for (const a of document.querySelectorAll<HTMLAnchorElement>("a[data-legal]")) {
  const url = a.getAttribute("href") ?? "";
  if (!checked.has(url)) {
    checked.set(url, fetch(url, { method: "HEAD", redirect: "manual" }).then((r) => r.ok || r.type === "opaqueredirect", () => false));
  }
  void checked.get(url)!.then((ok) => {
    if (ok) return;
    // Inside running text keep the words, drop only the link.
    if (a.closest("p, span:not(.chips)") && !a.closest("footer, .cta")) a.replaceWith(...a.childNodes);
    else a.remove();
  });
}

for (const b of document.querySelectorAll<HTMLButtonElement>("button[data-copy]")) {
  b.addEventListener("click", async () => {
    const text = document.getElementById(b.dataset.copy ?? "")?.innerText ?? "";
    const label = b.querySelector("span");
    try {
      await navigator.clipboard.writeText(text);
      if (label && !b.dataset.busy) {
        // Feedback: the label and the icon say it worked, then switch back.
        b.dataset.busy = "1";
        const before = label.textContent;
        const ico = b.querySelector("svg");
        const check = createElement(Check);
        for (const a of ["width", "height", "aria-hidden", "class"]) check.setAttribute(a, ico?.getAttribute(a) ?? "");
        label.textContent = b.dataset.done ?? before;
        ico?.replaceWith(check);
        setTimeout(() => {
          label.textContent = before;
          if (ico) check.replaceWith(ico);
          delete b.dataset.busy;
        }, 1600);
      }
    } catch {
      /* clipboard not allowed: the text stays selectable */
    }
  });
}

// Hero: a small knowledge graph; each question highlights the path the AI
// takes. Positions are language independent, labels come from the page.
type NetText = { nodes: Record<string, [string, string]>; questions: { label: string; answer: string; trace: string }[] };
const N: Record<string, [number, number]> = {
  kunde: [80, 50], vertrag: [320, 38], backup: [560, 50],
  kontakt: [80, 180], skill: [300, 168], server: [560, 180],
  onboarding: [80, 310], jonas: [320, 310], ups: [560, 310],
};
const E: [string, string][] = [
  ["kunde", "vertrag"], ["vertrag", "server"], ["kunde", "kontakt"], ["server", "backup"], ["skill", "server"],
  ["server", "ups"], ["jonas", "server"], ["onboarding", "jonas"], ["skill", "jonas"], ["kontakt", "skill"],
];
const Q = [
  { hot: ["kunde", "vertrag", "server", "skill", "kontakt"], path: [0, 1, 2, 4], fix: ["backup"] },
  { hot: ["ups", "server", "backup", "vertrag", "kunde"], path: [5, 3, 1, 0], fix: [] as string[] },
  { hot: ["onboarding", "jonas", "server"], path: [7, 6], fix: [] as string[] },
];
const SVG = "http://www.w3.org/2000/svg";

function el<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number>, text?: string) {
  const e = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  if (text !== undefined) e.textContent = text;
  return e;
}

const data = document.getElementById("net-data");
const svg = document.getElementById("netSvg");
const qbox = document.getElementById("netQ");
if (data && svg && qbox) {
  const text = JSON.parse(data.textContent ?? "{}") as NetText;
  const W = 150, H = 44;
  // Built once; a question only switches classes, so the colour transitions
  // in site.css show the path changing instead of the graph being redrawn.
  const edges = E.map(([a, b]) => {
    const [x1, y1] = N[a], [x2, y2] = N[b];
    const line = el("line", { class: "edge", x1, y1, x2, y2 });
    svg.append(line);
    return line;
  });
  const nodes: Record<string, SVGGElement> = {};
  for (const [k, [x, y]] of Object.entries(N)) {
    const [label, sub] = text.nodes[k] ?? [k, ""];
    const g = el("g", { class: "node" });
    g.append(el("rect", { x: x - W / 2, y: y - H / 2, width: W, height: H, rx: 8 }));
    g.append(el("text", { class: "t", x: x - W / 2 + 10, y: y - 3 }, label));
    g.append(el("text", { class: "s", x: x - W / 2 + 10, y: y + 13 }, sub));
    svg.append(g);
    nodes[k] = g;
  }
  const answer = document.getElementById("netA")!;
  const draw = (qi: number, first = false) => {
    const q = Q[qi];
    edges.forEach((line, i) => line.classList.toggle("hot", q.path.includes(i)));
    for (const [k, g] of Object.entries(nodes)) {
      g.classList.toggle("hot", q.hot.includes(k));
      g.classList.toggle("fix", !q.hot.includes(k) && q.fix.includes(k));
    }
    const t = text.questions[qi];
    answer.innerHTML = t.answer;
    document.getElementById("netT")!.textContent = t.trace;
    if (!first) answer.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" });
    qbox.querySelectorAll("button").forEach((b, i) => b.setAttribute("aria-pressed", String(i === qi)));
  };
  text.questions.forEach((t, i) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "qbtn";
    b.textContent = t.label;
    b.addEventListener("click", () => draw(i));
    qbox.append(b);
  });
  draw(0, true);
  // The flowing path only runs while the demo is on screen.
  const net = svg.closest(".net");
  if (net && "IntersectionObserver" in window) {
    new IntersectionObserver(([e]) => net.classList.toggle("off", !e.isIntersecting)).observe(net);
  }
}

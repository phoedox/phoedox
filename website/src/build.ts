// Builds the website's language versions: website/index.html (English) and
// website/de/index.html only name their language; the page itself is
// website/template.html with [[key]] placeholders filled from website/i18n.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import type { Plugin } from "vite";

const dir = dirname(fileURLToPath(import.meta.url));
const marker = /<!-- phx-website:([a-z]{2})\b/;

export function render(template: string, dict: Record<string, unknown>): string {
  const lang = String(dict.lang);
  return template.replace(/\[\[([@a-z0-9.:]+)\]\]/gi, (_, key: string) => {
    if (key === "@net") return JSON.stringify(dict.net).replace(/</g, "\\u003c");
    if (key.startsWith("cur:")) return key.slice(4) === lang ? 'aria-current="true"' : "";
    if (key === "home.href") return lang === "en" ? "/" : `/${lang}/`;
    const v = dict[key];
    if (typeof v !== "string") throw new Error(`website: missing text "${key}" for ${lang}`);
    return v;
  });
}

export function websitePages(): Plugin {
  return {
    name: "phoedox-website",
    transformIndexHtml: {
      order: "pre",
      handler(html) {
        const m = marker.exec(html);
        if (!m) return html;
        const template = readFileSync(join(dir, "template.html"), "utf8");
        const dict = JSON.parse(readFileSync(join(dir, "i18n", `${m[1]}.json`), "utf8"));
        return render(template, dict);
      },
    },
  };
}

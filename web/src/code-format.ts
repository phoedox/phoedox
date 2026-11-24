// Formatting code with Prettier (loaded only when someone formats) and the
// language of a file by its name. Used by the code editor (phx-code-editor).

export type CodeLang = "html" | "css" | "js" | "jsx" | "ts" | "tsx" | "json" | "md" | "text";

/** The language of a file by its extension (or a language name). */
export function langOf(name: string): CodeLang {
  const ext = (name.includes(".") ? name.split(".").pop() ?? "" : name).toLowerCase();
  switch (ext) {
    case "html": case "htm": case "svg": case "xml": return "html";
    case "css": return "css";
    case "js": case "mjs": case "cjs": return "js";
    case "jsx": return "jsx";
    case "ts": case "mts": return "ts";
    case "tsx": return "tsx";
    case "json": case "webmanifest": return "json";
    case "md": case "markdown": return "md";
    default: return "text";
  }
}

/** Whether Prettier can format the language. */
export const formattable = (lang: CodeLang) => lang !== "text";

/** Formats code (2 spaces, 100 columns); throws with Prettier's message on a syntax error. */
export async function formatCode(code: string, lang: CodeLang): Promise<string> {
  if (!formattable(lang)) return code;
  const prettier = await import("prettier/standalone");
  const parser = { html: "html", css: "css", js: "babel", jsx: "babel", ts: "typescript", tsx: "typescript", json: "json", md: "markdown" }[lang];
  const plugins = await Promise.all(
    lang === "html" ? [import("prettier/plugins/html"), import("prettier/plugins/postcss"), import("prettier/plugins/babel"), import("prettier/plugins/estree")]
    : lang === "css" ? [import("prettier/plugins/postcss")]
    : lang === "md" ? [import("prettier/plugins/markdown"), import("prettier/plugins/babel"), import("prettier/plugins/estree")]
    : lang === "ts" || lang === "tsx" ? [import("prettier/plugins/typescript"), import("prettier/plugins/estree")]
    : [import("prettier/plugins/babel"), import("prettier/plugins/estree")],
  );
  return prettier.format(code, {
    parser, plugins: plugins.map((p) => ("default" in p ? p.default : p)) as never[],
    printWidth: 100, tabWidth: 2, proseWrap: "preserve",
    filepath: lang === "tsx" ? "a.tsx" : lang === "jsx" ? "a.jsx" : undefined,
  });
}

// Builds the libraries apps import by name (internal/core/apps.go AppLibs)
// into public/applib as ES modules. React is one shared module; the others
// import it. Run after changing versions: bun scripts/applibs.ts
import { mkdir, writeFile, rm } from "node:fs/promises";

const out = "public/applib";
await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });
const tmp = "node_modules/.applib";
await mkdir(tmp, { recursive: true });

// React, its JSX runtime and react-dom/client share one bundle, so there is
// one React; the three import names are thin modules with named exports.
const react = await import("react");
const jsx = await import("react/jsx-runtime");
const dom = await import("react-dom/client");
const names = (m: object) => Object.keys(m).filter((k) => k !== "default" && /^[A-Za-z_$][\w$]*$/.test(k));
const thin = (key: string, m: object) => `import { ${key} as M } from "./react-all.js"; export default M; export const { ${names(m).join(", ")} } = M;\n`;

const entries: Record<string, string> = {
  "react-all.js": `import * as R from "react"; import * as J from "react/jsx-runtime"; import * as D from "react-dom/client"; export const React = R; export const JSX = J; export const DOM = D;`,
  "lit.js": `export * from "lit";`,
  "chart.js": `export * from "chart.js/auto"; export { default } from "chart.js/auto";`,
  "d3.js": `export * from "d3";`,
};

for (const [name, code] of Object.entries(entries)) {
  const src = `${tmp}/${name}`;
  await writeFile(src, code);
  const r = await Bun.build({ entrypoints: [src], format: "esm", minify: true, target: "browser", define: { "process.env.NODE_ENV": '"production"' } });
  if (!r.success) throw new Error(`${name}: ${r.logs.join("\n")}`);
  await writeFile(`${out}/${name}`, await r.outputs[0].text());
}
await writeFile(`${out}/react.js`, thin("React", react));
await writeFile(`${out}/react-jsx-runtime.js`, thin("JSX", jsx));
await writeFile(`${out}/react-dom-client.js`, thin("DOM", dom));
console.log("built", Object.keys(entries).join(", "), "+ react thin modules");

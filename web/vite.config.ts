import { defineConfig } from "vite";

// The Go server embeds web/dist. In dev, Vite proxies API, MCP and realtime.
export default defineConfig({
  build: {
    outDir: "dist", emptyOutDir: true, target: "es2022",
    // The server reads the manifest to link the islands of published sites with their CSS.
    manifest: true,
    rollupOptions: {
      // The app and the islands for published sites (src/pub, fixed names; the server adds ?v=version).
      input: { main: "index.html", pub: "src/pub/main.ts", website: "website/index.html" },
      output: {
        entryFileNames: (c) => (c.name === "pub" ? "pub/pub.js" : "assets/[name]-[hash].js"),
        assetFileNames: (a) => (a.names?.includes("pub.css") ? "pub/pub.css" : "assets/[name]-[hash][extname]"),
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/mcp": "http://localhost:8080",
      "/rt": { target: "ws://localhost:8080", ws: true },
      "/healthz": "http://localhost:8080",
    },
  },
});

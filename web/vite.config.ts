import { defineConfig } from "vite";

// The Go server embeds web/dist. In dev, Vite proxies API, MCP and realtime.
export default defineConfig({
  build: { outDir: "dist", emptyOutDir: true, target: "es2022" },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/mcp": "http://localhost:8080",
      "/rt": { target: "ws://localhost:8080", ws: true },
      "/healthz": "http://localhost:8080",
    },
  },
});

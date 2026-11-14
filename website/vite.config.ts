import { defineConfig } from "vite";
import { websitePages } from "./src/build.ts";

// The product website (ADR-0054): static pages per language, served by
// cmd/phoedox-website, which embeds website/dist.
export default defineConfig({
  plugins: [websitePages()],
  build: {
    outDir: "dist", emptyOutDir: true, target: "es2022",
    rollupOptions: { input: { en: "index.html", de: "de/index.html" } },
  },
});

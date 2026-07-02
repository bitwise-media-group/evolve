import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import { viteSingleFile } from "vite-plugin-singlefile";

// The Go server (`evolve view`) hosts the read-only API. For `npm run dev`, run
// it on a fixed port and point the proxy at it:
//
//   evolve view --port 8099 --no-open      # in one terminal
//   VITE_API_TARGET=http://127.0.0.1:8099 npm run dev
//
// The production build is a single self-contained index.html (viteSingleFile),
// which the Go binary embeds and the snapshot export inlines.
const apiTarget = process.env.VITE_API_TARGET || "http://127.0.0.1:8099";

export default defineConfig({
  plugins: [preact(), viteSingleFile()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 4096,
  },
  server: {
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true },
      "/events": { target: apiTarget, changeOrigin: true },
    },
  },
});

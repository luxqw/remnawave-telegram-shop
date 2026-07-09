import { defineConfig } from "vite";
import preact from "@preact/preset-vite";

// Local dev: `npm run dev` runs Vite's dev server and proxies /admin/api/* to the Go server
// (started separately via `go run ./cmd/app`) so the SPA and API share an origin exactly like
// they do in production once //go:embed serves the built assets.
export default defineConfig({
  plugins: [preact()],
  base: "/admin/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/admin/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});

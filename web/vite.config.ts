import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The build goes into the Go package that embeds it. Vite empties that folder first, so put back the placeholder
// that lets `go build` work before the app has been built.
const outDir = fileURLToPath(new URL("../internal/web/dist", import.meta.url));

export default defineConfig({
  plugins: [
    react(),
    {
      name: "keep-embed-placeholder",
      closeBundle() {
        writeFileSync(`${outDir}/.gitkeep`, "");
      },
    },
  ],
  build: { outDir, emptyOutDir: true },
  server: {
    // `npm run dev` serves the app with hot reload; the API is the running Go process.
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
  test: { environment: "node" },
});

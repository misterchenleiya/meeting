import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const rootDir = fileURLToPath(new URL(".", import.meta.url));

export default defineConfig({
  plugins: [react()],
  cacheDir: path.resolve(rootDir, "../build/.cache/vite"),
  build: {
    outDir: path.resolve(rootDir, "../build/frontend"),
    emptyOutDir: true
  },
  server: {
    port: 5188,
    host: "0.0.0.0",
    proxy: {
      "/api": "http://127.0.0.1:5180",
      "/healthz": "http://127.0.0.1:5180"
    }
  }
});

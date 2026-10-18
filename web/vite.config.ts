import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// semar-api address for the dev server proxy (default: local `make run-api`
// or the Lite profile).
const apiTarget = process.env.SEMAR_API_URL ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  // The proxy must keep the browser's Host header: semar-api compares the
  // Origin of state-changing requests with Host (CSRF protection), and
  // Vite's string shorthand would rewrite Host to the target (changeOrigin),
  // so every POST, including login, would be refused. preview.proxy
  // defaults to server.proxy, so `vite preview` behaves the same.
  server: {
    // ws: console WebSockets live under /api.
    proxy: Object.fromEntries(
      ["/api", "/healthz", "/readyz"].map((path) => [path, { target: apiTarget, changeOrigin: false, ws: path === "/api" }]),
    ),
  },
  build: {
    // The console page (noVNC + xterm.js, ~530 kB) is loaded on demand;
    // the limit is set just above it so other chunks still warn.
    chunkSizeWarningLimit: 600,
  },
  test: {
    environment: "jsdom",
  },
});

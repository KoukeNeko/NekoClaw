import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  server: {
    proxy: {
      "/v1": "http://127.0.0.1:8085",
      "/healthz": "http://127.0.0.1:8085",
      "/oauth2callback": "http://127.0.0.1:8085",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});

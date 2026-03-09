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
      "/v1": {
        target: "http://127.0.0.1:8085",
        xfwd: true,
      },
      "/healthz": {
        target: "http://127.0.0.1:8085",
        xfwd: true,
      },
      "/oauth2callback": {
        target: "http://127.0.0.1:8085",
        xfwd: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});

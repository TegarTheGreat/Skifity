import path from "node:path"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  build: {
    // The whole build is embedded in the Go binary, so keeping it small keeps
    // the binary small. Source maps would roughly double it.
    sourcemap: false,
    // Panel assets are served from the same origin as the API and are
    // fingerprinted, so they can be cached for a year.
    assetsDir: "assets",
    // web/dist is tracked (empty) so that the Go embed compiles in a fresh
    // clone. Emptying it here would delete the .gitignore that keeps it, so
    // the build script clears the assets directory instead.
    emptyOutDir: false,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        manualChunks: {
          // Five languages are bundled up front so switching one is instant.
          // They are a fifth of the bundle, so they get their own chunk that
          // the browser can cache across panel upgrades.
          i18n: ["i18next", "react-i18next", "i18next-browser-languagedetector"],
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      // In development the Go binary serves the API and proxies everything
      // else here, but running Vite on its own must work too.
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: false,
      },
    },
  },
})

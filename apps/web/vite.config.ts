import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { tanstackStart } from "@tanstack/react-start/plugin/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, loadEnv } from "vite"

const repoRoot = path.resolve(import.meta.dirname, "../..")

export default defineConfig(({ mode }) => {
  // One .env at the repo root is shared with the API. Only API_PORT is read here,
  // and it stays on the server side: nothing from .env reaches the browser.
  const env = loadEnv(mode, repoRoot, "")
  const apiPort = env.API_PORT || "8080"

  return {
    envDir: repoRoot,
    envPrefix: "STREAKS_PUBLIC_",
    resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
    server: {
      port: 5173,
      proxy: {
        "/api": {
          target: `http://127.0.0.1:${apiPort}`,
          rewrite: (p: string) => p.replace(/^\/api/, ""),
        },
      },
    },
    plugins: [
      tailwindcss(),
      tanstackStart({
        // index.html, so static hosts (the Cloudflare Worker) serve it for every route.
        spa: { enabled: true, prerender: { outputPath: "/index", autoSubfolderIndex: false } },
      }),
      react(),
    ],
  }
})

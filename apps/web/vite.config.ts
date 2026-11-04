import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { tanstackStart } from "@tanstack/react-start/plugin/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, loadEnv } from "vite"

const repoRoot = path.resolve(import.meta.dirname, "../..")

export default defineConfig(({ mode }) => {
  // One .env at the repo root is shared with the API.
  const env = loadEnv(mode, repoRoot, "")
  const apiPort = env.API_PORT || "8080"

  return {
    envDir: repoRoot,
    resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
    define: {
      // Ends up in the browser bundle: see "Security" in the README.
      "import.meta.env.VITE_API_SECRET": JSON.stringify(env.API_SECRET ?? ""),
    },
    server: {
      port: 5173,
      proxy: {
        "/api": {
          target: `http://127.0.0.1:${apiPort}`,
          rewrite: (p: string) => p.replace(/^\/api/, ""),
        },
      },
    },
    plugins: [tailwindcss(), tanstackStart({ spa: { enabled: true } }), react()],
  }
})

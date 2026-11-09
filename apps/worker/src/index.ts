// Streaks' front door: static app, /api proxy, and the cron that wakes the API.

const REMINDERS_CRON = "30,45 5,11,16 * * *"
const SYNC_CRON = "0 */6 * * *"

export default {
  async fetch(request, env) {
    const url = new URL(request.url)
    if (url.pathname !== "/api" && !url.pathname.startsWith("/api/")) {
      return env.ASSETS.fetch(request)
    }
    const path = url.pathname.slice("/api".length) || "/"
    // The jobs routes are for the cron below, not the public.
    if (path.startsWith("/jobs/")) {
      return new Response("Not found", { status: 404 })
    }
    const target = new URL(path + url.search, env.API_ORIGIN)
    const headers = new Headers(request.headers)
    headers.delete("Authorization")
    return fetch(target, {
      method: request.method,
      headers,
      body: request.body,
      // Pass redirects (OAuth, login) back to the browser rather than following them.
      redirect: "manual",
    })
  },

  async scheduled(controller, env) {
    let path: string
    switch (controller.cron) {
      case REMINDERS_CRON:
        path = "/jobs/reminders"
        break
      case SYNC_CRON:
        path = "/jobs/sync"
        break
      default: {
        // Keep-awake ping. /healthz doesn't touch the database, so Neon still sleeps.
        const res = await fetch(new URL("/healthz", env.API_ORIGIN))
        await res.body?.cancel()
        return
      }
    }
    const res = await fetch(new URL(path, env.API_ORIGIN), {
      method: "POST",
      headers: { Authorization: `Bearer ${env.JOBS_SECRET}` },
    })
    const body = await res.text()
    if (!res.ok) {
      // Throwing marks the cron run as failed in the Cloudflare dashboard.
      throw new Error(`${path}: HTTP ${res.status}: ${body.slice(0, 500)}`)
    }
    console.log(path, body)
  },
} satisfies ExportedHandler<Env>

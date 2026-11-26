// Streaks' front door: static app, /api proxy, and the cron that wakes the API.

const REMINDERS_CRON = "30,45 5,11,16 * * *"
const SYNC_CRON = "0 */6 * * *"

// Render's free plan sleeps and takes up to a minute to wake. While it does, the
// edge answers 502/503/504, so safe requests are retried instead of failing.
const WAKE_STATUSES = new Set([502, 503, 504])
const WAKE_BUDGET_MS = 55_000
const WAKE_RETRY_MS = 3_000
// Render holds the connection while booting and answers within a minute, but a
// connection that never answers would pin the Worker forever without this.
const ATTEMPT_TIMEOUT_MS = 60_000

async function fetchWakingApi(target: URL, init: RequestInit, retry: boolean): Promise<Response> {
  const deadline = Date.now() + WAKE_BUDGET_MS
  for (;;) {
    try {
      const res = await fetch(target, { ...init, signal: AbortSignal.timeout(ATTEMPT_TIMEOUT_MS) })
      if (!retry || !WAKE_STATUSES.has(res.status) || Date.now() + WAKE_RETRY_MS > deadline) return res
      await res.body?.cancel()
    } catch (err) {
      if (!retry || Date.now() + WAKE_RETRY_MS > deadline) throw err
    }
    await new Promise((resolve) => setTimeout(resolve, WAKE_RETRY_MS))
  }
}

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
    const safe = request.method === "GET" || request.method === "HEAD"
    return fetchWakingApi(
      target,
      {
        method: request.method,
        headers,
        body: request.body,
        // Pass redirects (OAuth, login) back to the browser rather than following them.
        redirect: "manual",
      },
      safe,
    )
  },

  async scheduled(controller, env) {
    const started = Date.now()
    let path: string
    switch (controller.cron) {
      case REMINDERS_CRON:
        path = "/jobs/reminders"
        break
      case SYNC_CRON:
        path = "/jobs/sync"
        break
      default: {
        // Keep-awake ping, with the same wake retries as a browser request: a
        // ping that lands on a sleeping Render stays until the API answers.
        // /healthz doesn't touch the database, so Neon still sleeps.
        const res = await fetchWakingApi(new URL("/healthz", env.API_ORIGIN), {}, true)
        await res.body?.cancel()
        if (!res.ok) {
          // Throwing marks the cron run as failed in the Cloudflare dashboard.
          throw new Error(`keep-awake: HTTP ${res.status}`)
        }
        console.log("keep-awake", res.status, `${Date.now() - started}ms`)
        return
      }
    }
    // Both jobs are safe to re-run: reminder sends are claimed once per window
    // and syncs upsert, so retrying while Render wakes can't double-send.
    const res = await fetchWakingApi(
      new URL(path, env.API_ORIGIN),
      {
        method: "POST",
        headers: { Authorization: `Bearer ${env.JOBS_SECRET}` },
      },
      true,
    )
    const body = await res.text()
    if (!res.ok) {
      // Throwing marks the cron run as failed in the Cloudflare dashboard.
      throw new Error(`${path}: HTTP ${res.status}: ${body.slice(0, 500)}`)
    }
    console.log(path, body, `${Date.now() - started}ms`)
  },
} satisfies ExportedHandler<Env>

// Streaks' front door: serves the static app and forwards /api to the Go API.
//
// The API runs on Render's free plan, which sleeps when idle. A sleeping Render
// answers requests from a Worker with an instant 502 and never boots for them
// (Cloudflare analytics showed every Worker request getting one for 20 hours),
// but a request straight from a browser or a server wakes it within a minute.
// So the Worker can't wake the API itself. Instead it tells the browser where to
// knock, in the X-Api-Wake header, and the browser wakes it (see
// apps/web/src/lib/wake.ts). Scheduled jobs run from GitHub Actions for the same
// reason (.github/workflows/scheduled-jobs.yml).

// What Render's edge answers while the API is asleep or still booting.
const WAKE_STATUSES = new Set([502, 503, 504])
// A connection that never answers would otherwise hold the request forever.
const ATTEMPT_TIMEOUT_MS = 60_000

export const WAKE_HEADER = "X-Api-Wake"

// Marks a response as "the API is asleep", with the URL that wakes it.
function asleep(res: Response, env: Env): Response {
  const out = new Response(res.body, res)
  out.headers.set(WAKE_HEADER, new URL("/healthz", env.API_ORIGIN).toString())
  out.headers.set("Cache-Control", "no-store")
  return out
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url)
    if (url.pathname !== "/api" && !url.pathname.startsWith("/api/")) {
      return env.ASSETS.fetch(request)
    }
    const path = url.pathname.slice("/api".length) || "/"
    // The jobs routes are for the scheduled workflow, which calls Render directly.
    if (path.startsWith("/jobs/")) {
      return new Response("Not found", { status: 404 })
    }
    const headers = new Headers(request.headers)
    headers.delete("Authorization")
    let res: Response
    try {
      res = await fetch(new URL(path + url.search, env.API_ORIGIN), {
        method: request.method,
        headers,
        body: request.body,
        // Pass redirects (OAuth, login) back to the browser rather than following them.
        redirect: "manual",
        signal: AbortSignal.timeout(ATTEMPT_TIMEOUT_MS),
      })
    } catch {
      // Timed out or the connection failed: treat it like a sleeping API.
      res = new Response(JSON.stringify({ error: "The API didn't answer." }), {
        status: 504,
        headers: { "Content-Type": "application/json" },
      })
    }
    return WAKE_STATUSES.has(res.status) ? asleep(res, env) : res
  },
} satisfies ExportedHandler<Env>

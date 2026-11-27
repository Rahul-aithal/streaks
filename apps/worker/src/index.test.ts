import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import worker, { WAKE_HEADER } from "./index"

const env = {
  API_ORIGIN: "https://api.test",
  ASSETS: { fetch: vi.fn() },
} as unknown as Env

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

const call = (req: Request) => worker.fetch(req as never, env)

describe("fetch", () => {
  it("serves non-API paths from assets", async () => {
    vi.mocked(env.ASSETS.fetch).mockResolvedValue(new Response("page"))
    const res = await call(new Request("https://site.test/habits"))
    expect(await res.text()).toBe("page")
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("doesn't treat /apix as the API", async () => {
    vi.mocked(env.ASSETS.fetch).mockResolvedValue(new Response("page"))
    await call(new Request("https://site.test/apix"))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("hides the jobs routes", async () => {
    const res = await call(new Request("https://site.test/api/jobs/sync", { method: "POST" }))
    expect(res.status).toBe(404)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("proxies /api/x to the origin without Authorization or redirect following", async () => {
    fetchMock.mockResolvedValue(new Response("ok"))
    const req = new Request("https://site.test/api/auth/me?a=1", { headers: { Authorization: "Bearer x", Cookie: "c=1" } })
    const res = await call(req)
    expect(await res.text()).toBe("ok")
    expect(res.headers.get(WAKE_HEADER)).toBeNull()
    const [target, init] = fetchMock.mock.calls[0]
    expect(String(target)).toBe("https://api.test/auth/me?a=1")
    expect(init.redirect).toBe("manual")
    expect(init.signal).toBeInstanceOf(AbortSignal)
    expect(init.headers.get("Authorization")).toBeNull()
    expect(init.headers.get("Cookie")).toBe("c=1")
  })

  it("maps bare /api to the origin root", async () => {
    fetchMock.mockResolvedValue(new Response("root"))
    await call(new Request("https://site.test/api"))
    expect(String(fetchMock.mock.calls[0][0])).toBe("https://api.test/")
  })

  it("passes redirects and 4xx through untouched", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 302, headers: { Location: "/login" } }))
    let res = await call(new Request("https://site.test/api/oauth/google/callback"))
    expect(res.status).toBe(302)
    expect(res.headers.get("Location")).toBe("/login")
    fetchMock.mockResolvedValueOnce(new Response("{}", { status: 401 }))
    res = await call(new Request("https://site.test/api/auth/me"))
    expect(res.status).toBe(401)
    expect(res.headers.get(WAKE_HEADER)).toBeNull()
  })

  it.each([502, 503, 504])("answers a sleeping API's %i at once, with where to wake it", async (status) => {
    fetchMock.mockResolvedValue(new Response("<html>asleep</html>", { status }))
    const res = await call(new Request("https://site.test/api/summary/today"))
    expect(res.status).toBe(status)
    expect(res.headers.get(WAKE_HEADER)).toBe("https://api.test/healthz")
    expect(res.headers.get("Cache-Control")).toBe("no-store")
    expect(await res.text()).toBe("<html>asleep</html>")
    // No retry loop: a sleeping Render never boots for a Worker's request.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("turns a failed or timed-out connection into a 504 with where to wake it", async () => {
    fetchMock.mockRejectedValue(new DOMException("timed out", "TimeoutError"))
    const res = await call(new Request("https://site.test/api/habits", { method: "POST", body: "{}" }))
    expect(res.status).toBe(504)
    expect(res.headers.get(WAKE_HEADER)).toBe("https://api.test/healthz")
    expect(await res.json()).toEqual({ error: "The API didn't answer." })
  })
})

it("has no scheduled handler: jobs run from GitHub Actions", () => {
  expect(worker).not.toHaveProperty("scheduled")
})

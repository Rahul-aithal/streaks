import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import worker from "./index"

const env = {
  API_ORIGIN: "https://api.test",
  JOBS_SECRET: "s3cret",
  ASSETS: { fetch: vi.fn() },
} as unknown as Env

const fetchMock = vi.fn()

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal("fetch", fetchMock)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

const call = (req: Request) => worker.fetch!(req as never, env)
const cron = (expr: string) => worker.scheduled!({ cron: expr } as ScheduledController, env)

describe("fetch", () => {
  it("serves non-API paths from assets", async () => {
    vi.mocked(env.ASSETS.fetch).mockResolvedValue(new Response("page"))
    const res = await call(new Request("https://site.test/habits"))
    expect(await res.text()).toBe("page")
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
    const [target, init] = fetchMock.mock.calls[0]
    expect(String(target)).toBe("https://api.test/auth/me?a=1")
    expect(init.redirect).toBe("manual")
    expect(init.headers.get("Authorization")).toBeNull()
    expect(init.headers.get("Cookie")).toBe("c=1")
  })

  it("maps bare /api to the origin root", async () => {
    fetchMock.mockResolvedValue(new Response("root"))
    await call(new Request("https://site.test/api"))
    expect(String(fetchMock.mock.calls[0][0])).toBe("https://api.test/")
  })

  it("retries a GET while the API wakes up, then returns the good response", async () => {
    fetchMock
      .mockResolvedValueOnce(new Response("", { status: 504 }))
      .mockRejectedValueOnce(new TypeError("connection reset"))
      .mockResolvedValueOnce(new Response("awake"))
    const pending = call(new Request("https://site.test/api/auth/me"))
    await vi.advanceTimersByTimeAsync(10_000)
    const res = await pending
    expect(await res.text()).toBe("awake")
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it("gives up after the wake budget and returns the last 5xx", async () => {
    fetchMock.mockImplementation(async () => new Response("", { status: 503 }))
    const pending = call(new Request("https://site.test/api/auth/me"))
    await vi.advanceTimersByTimeAsync(60_000)
    expect((await pending).status).toBe(503)
    expect(fetchMock.mock.calls.length).toBeGreaterThan(5)
    expect(fetchMock.mock.calls.length).toBeLessThan(25)
  })

  it("rethrows a network error once the budget is spent", async () => {
    fetchMock.mockImplementation(async () => {
      throw new TypeError("down")
    })
    const pending = call(new Request("https://site.test/api/auth/me"))
    const assertion = expect(pending).rejects.toThrow("down")
    await vi.advanceTimersByTimeAsync(60_000)
    await assertion
  })

  it("does not retry non-GET requests", async () => {
    fetchMock.mockResolvedValue(new Response("", { status: 504 }))
    const res = await call(new Request("https://site.test/api/habits", { method: "POST", body: "{}" }))
    expect(res.status).toBe(504)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("does not retry a non-GET network error", async () => {
    fetchMock.mockRejectedValue(new TypeError("down"))
    await expect(call(new Request("https://site.test/api/habits", { method: "POST", body: "{}" }))).rejects.toThrow("down")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("does not retry a GET that fails with a 4xx", async () => {
    fetchMock.mockResolvedValue(new Response("", { status: 401 }))
    const res = await call(new Request("https://site.test/api/auth/me"))
    expect(res.status).toBe(401)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

describe("scheduled", () => {
  it("posts the reminders job with the secret", async () => {
    fetchMock.mockResolvedValue(new Response("sent"))
    const log = vi.spyOn(console, "log").mockImplementation(() => {})
    await cron("30,45 5,11,16 * * *")
    const [target, init] = fetchMock.mock.calls[0]
    expect(String(target)).toBe("https://api.test/jobs/reminders")
    expect(init.method).toBe("POST")
    expect(init.headers.Authorization).toBe("Bearer s3cret")
    expect(log).toHaveBeenCalledWith("/jobs/reminders", "sent", "0ms")
  })

  it("posts the sync job", async () => {
    fetchMock.mockResolvedValue(new Response("synced"))
    vi.spyOn(console, "log").mockImplementation(() => {})
    await cron("0 */6 * * *")
    expect(String(fetchMock.mock.calls[0][0])).toBe("https://api.test/jobs/sync")
  })

  it("retries a job while the API wakes up, then succeeds", async () => {
    fetchMock
      .mockResolvedValueOnce(new Response("", { status: 503 }))
      .mockResolvedValueOnce(new Response("synced"))
    vi.spyOn(console, "log").mockImplementation(() => {})
    const pending = cron("0 */6 * * *")
    await vi.advanceTimersByTimeAsync(10_000)
    await pending
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("fails the run when a job returns an error", async () => {
    fetchMock.mockResolvedValue(new Response("boom", { status: 500 }))
    await expect(cron("0 */6 * * *")).rejects.toThrow("/jobs/sync: HTTP 500: boom")
  })

  it("pings /healthz for the keep-awake cron", async () => {
    fetchMock.mockResolvedValue(new Response("ok"))
    const log = vi.spyOn(console, "log").mockImplementation(() => {})
    await cron("*/14 1-18 * * *")
    expect(String(fetchMock.mock.calls[0][0])).toBe("https://api.test/healthz")
    expect(log).toHaveBeenCalledWith("keep-awake", 200, "0ms")
  })

  it("keeps pinging until a sleeping API answers", async () => {
    fetchMock.mockReset()
    fetchMock
      .mockResolvedValueOnce(new Response("", { status: 502 }))
      .mockRejectedValueOnce(new TypeError("connection reset"))
      .mockResolvedValueOnce(new Response("ok"))
    vi.spyOn(console, "log").mockImplementation(() => {})
    const pending = cron("*/14 1-18 * * *")
    await vi.advanceTimersByTimeAsync(10_000)
    await pending
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it("fails the run when the API never answers the ping", async () => {
    fetchMock.mockReset()
    fetchMock.mockImplementation(async () => new Response("", { status: 503 }))
    const assertion = expect(cron("*/14 1-18 * * *")).rejects.toThrow("keep-awake: HTTP 503")
    await vi.advanceTimersByTimeAsync(60_000)
    await assertion
  })

  it("tolerates a ping with no body", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    vi.spyOn(console, "log").mockImplementation(() => {})
    await cron("*/14 1-18 * * *")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

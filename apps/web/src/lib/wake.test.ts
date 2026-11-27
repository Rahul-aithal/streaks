import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { WAKE_HEADER, wakeApi, wakeUrlOf } from "@/lib/wake"

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

const withHeader = (value?: string) =>
  new Response(null, { status: 502, headers: value === undefined ? {} : { [WAKE_HEADER]: value } })

describe("wakeUrlOf", () => {
  it("reads the https wake URL the Worker set", () => {
    expect(wakeUrlOf(withHeader("https://api.test/healthz"))).toBe("https://api.test/healthz")
  })

  it("ignores a missing, non-https or broken header", () => {
    expect(wakeUrlOf(withHeader())).toBeNull()
    expect(wakeUrlOf(withHeader("http://api.test/healthz"))).toBeNull()
    expect(wakeUrlOf(withHeader("not a url"))).toBeNull()
  })
})

describe("wakeApi", () => {
  it("calls the host straight from the browser, without cookies, and waits for it", async () => {
    let answer!: () => void
    fetchMock.mockReturnValue(new Promise((resolve) => (answer = () => resolve(new Response()))))
    let done = false
    const pending = wakeApi("https://api.test/healthz").then(() => (done = true))
    await Promise.resolve()
    expect(done).toBe(false) // Still booting.
    answer()
    await pending
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe("https://api.test/healthz")
    expect(init).toMatchObject({ mode: "no-cors", cache: "no-store", credentials: "omit" })
    expect(init.signal).toBeInstanceOf(AbortSignal)
  })

  it("shares one wake between requests that find the API asleep together", async () => {
    fetchMock.mockResolvedValue(new Response())
    await Promise.all([wakeApi("https://api.test/healthz"), wakeApi("https://api.test/healthz")])
    expect(fetchMock).toHaveBeenCalledTimes(1)
    // Once it's done, a later sleep gets a fresh wake.
    await wakeApi("https://api.test/healthz")
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("resolves even when the wake fails, so the request can report its own error", async () => {
    fetchMock.mockRejectedValue(new TypeError("offline"))
    await expect(wakeApi("https://api.test/healthz")).resolves.toBeUndefined()
  })
})

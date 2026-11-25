import { describe, expect, it } from "vitest"
import { ApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"

describe("describeError", () => {
  it("maps API statuses to a kind and whether to retry", () => {
    const cases: [number, string, boolean][] = [
      [401, "unauthorized", false],
      [403, "forbidden", false],
      [404, "not_found", false],
      [500, "server", true],
      [504, "server", true],
      [418, "unknown", true],
    ]
    for (const [status, kind, retryable] of cases) {
      const info = describeError(new ApiError("boom", status))
      expect(info.kind, String(status)).toBe(kind)
      expect(info.retryable, String(status)).toBe(retryable)
    }
  })

  it("explains the cold start for a 5xx", () => {
    expect(describeError(new ApiError("x", 503)).message).toMatch(/sleep/)
  })

  it("shows the API's own words for 403 and 404, and a plain fallback when it gave none", () => {
    expect(describeError(new ApiError("Google Tasks needs write access: reconnect", 403)).message).toMatch(/reconnect/)
    expect(describeError(new ApiError("task not found: sync and try again", 404)).message).toMatch(/sync/)
    expect(describeError(new ApiError("Request failed (403). Is the API running?", 403)).message).toMatch(/doesn't have access/)
    expect(describeError(new ApiError("Request failed (404). Is the API running?", 404)).message).toMatch(/doesn't exist/)
  })

  it("keeps the API's own message for other client errors", () => {
    expect(describeError(new ApiError("name is required", 400)).message).toBe("name is required")
  })

  it("treats a failed fetch as a network problem", () => {
    const info = describeError(new TypeError("Failed to fetch"))
    expect(info.kind).toBe("network")
    expect(info.retryable).toBe(true)
  })

  it("falls back to the error's message, then a generic one", () => {
    expect(describeError(new Error("render crashed")).message).toBe("render crashed")
    expect(describeError(new Error("")).message).toBe("An unexpected error happened.")
    expect(describeError("nope").message).toBe("An unexpected error happened.")
  })
})

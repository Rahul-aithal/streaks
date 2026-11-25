import { describe, expect, it } from "vitest"
import { ApiError } from "@/lib/api"
import { retryDelay, shouldRetry } from "@/lib/query-retry"

describe("shouldRetry", () => {
  it("never retries a 4xx, such as being signed out", () => {
    expect(shouldRetry(0, new ApiError("nope", 401))).toBe(false)
    expect(shouldRetry(0, new ApiError("bad", 400))).toBe(false)
  })

  it("retries a 5xx up to three times", () => {
    const err = new ApiError("down", 504)
    expect(shouldRetry(0, err)).toBe(true)
    expect(shouldRetry(2, err)).toBe(true)
    expect(shouldRetry(3, err)).toBe(false)
  })

  it("retries network errors, which are not ApiErrors", () => {
    expect(shouldRetry(0, new TypeError("Failed to fetch"))).toBe(true)
    expect(shouldRetry(3, new TypeError("Failed to fetch"))).toBe(false)
  })
})

describe("retryDelay", () => {
  it("backs off and caps at 10 seconds", () => {
    expect(retryDelay(0)).toBe(2_000)
    expect(retryDelay(1)).toBe(4_000)
    expect(retryDelay(2)).toBe(8_000)
    expect(retryDelay(3)).toBe(10_000)
  })
})

import { ApiError } from "@/lib/api"

// What Render's edge answers while the API is asleep or booting.
const WAKE_STATUSES = new Set([502, 503, 504])

// Retries ride out a Render or Neon cold start. A read that hit a sleeping API
// has already waited for the browser to wake it (lib/wake.ts) and been sent
// again, so it gets one more chance instead of three — otherwise a dead API
// keeps the page spinning for minutes. Other 5xx retry as before. A 4xx (being signed
// out, a bad request) won't change on retry.
export function shouldRetry(count: number, error: Error): boolean {
  if (error instanceof ApiError && error.status < 500) return false
  if (error instanceof ApiError && WAKE_STATUSES.has(error.status)) return count < 2
  return count < 3
}

export function retryDelay(attempt: number): number {
  return Math.min(2_000 * 2 ** attempt, 10_000)
}

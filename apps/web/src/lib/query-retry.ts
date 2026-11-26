import { ApiError } from "@/lib/api"

// What the Worker already rode out while Render wakes up.
const WAKE_STATUSES = new Set([502, 503, 504])

// Retries ride out a Render or Neon cold start. A wake status has already been
// retried by the Worker for up to 55 seconds before it reached the browser, so
// it gets one more chance instead of three — otherwise a sleeping API keeps the
// page spinning for minutes. Other 5xx retry as before. A 4xx (being signed
// out, a bad request) won't change on retry.
export function shouldRetry(count: number, error: Error): boolean {
  if (error instanceof ApiError && error.status < 500) return false
  if (error instanceof ApiError && WAKE_STATUSES.has(error.status)) return count < 2
  return count < 3
}

export function retryDelay(attempt: number): number {
  return Math.min(2_000 * 2 ** attempt, 10_000)
}

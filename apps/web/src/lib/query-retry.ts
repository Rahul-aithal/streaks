import { ApiError } from "@/lib/api"

// Retries ride out a Render or Neon cold start. A 4xx (being signed out, a bad
// request) won't change on retry.
export function shouldRetry(count: number, error: Error): boolean {
  if (error instanceof ApiError && error.status < 500) return false
  return count < 3
}

export function retryDelay(attempt: number): number {
  return Math.min(2_000 * 2 ** attempt, 10_000)
}

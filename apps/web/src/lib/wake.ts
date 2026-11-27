// Waking the API on Render's free plan, which sleeps when idle.
//
// A sleeping Render ignores requests from the Cloudflare Worker that fronts the
// API: it answers them with an instant 502 and doesn't boot. A request straight
// from the browser does boot it, and Render holds that request open until the
// API is up (about 20 seconds to a minute). So when the Worker says the API is
// asleep, the browser knocks on Render's door itself. See apps/worker/src/index.ts.

// Set by the Worker on a "the API is asleep" response: the URL that wakes it.
export const WAKE_HEADER = "X-Api-Wake"
const WAKE_TIMEOUT_MS = 120_000

let waking: Promise<void> | null = null

// The wake URL on a response, if the Worker marked the API as asleep. Only an
// https URL is accepted.
export function wakeUrlOf(res: Response): string | null {
  const url = res.headers.get(WAKE_HEADER)
  if (!url) return null
  try {
    return new URL(url).protocol === "https:" ? url : null
  } catch {
    return null
  }
}

// Calls the API host directly so it boots, and resolves once it answers (or
// gives up). Requests that find the API asleep at the same time share one wake.
// "no-cors": the answer can't be read cross-origin, and doesn't need to be.
export function wakeApi(url: string): Promise<void> {
  waking ??= fetch(url, {
    mode: "no-cors",
    cache: "no-store",
    credentials: "omit",
    signal: AbortSignal.timeout(WAKE_TIMEOUT_MS),
  })
    .then(
      () => undefined,
      () => undefined,
    )
    .finally(() => {
      waking = null
    })
  return waking
}

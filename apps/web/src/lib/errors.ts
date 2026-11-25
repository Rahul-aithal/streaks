import { ApiError } from "@/lib/api"

export type ErrorKind = "unauthorized" | "forbidden" | "not_found" | "server" | "network" | "unknown"

export interface ErrorInfo {
  kind: ErrorKind
  title: string
  message: string
  // Worth trying again: the server may just be waking up or the network blipped.
  retryable: boolean
}

// Turns whatever a request threw into something a person can act on.
// The API's own words when it gave any; its generic fallback isn't worth showing.
const apiWords = (error: ApiError, fallback: string) =>
  error.message.startsWith("Request failed") ? fallback : error.message

export function describeError(error: unknown): ErrorInfo {
  if (error instanceof ApiError) {
    switch (true) {
      case error.status === 401:
        return {
          kind: "unauthorized",
          title: "Please sign in",
          message: "You need to sign in to see this.",
          retryable: false,
        }
      case error.status === 403:
        return {
          kind: "forbidden",
          title: "No access",
          message: apiWords(error, "This account doesn't have access to that."),
          retryable: false,
        }
      case error.status === 404:
        return {
          kind: "not_found",
          title: "Not found",
          message: apiWords(error, "That doesn't exist, or it was deleted."),
          retryable: false,
        }
      case error.status >= 500:
        return {
          kind: "server",
          title: "The server isn't answering",
          message:
            "Free hosting puts the server to sleep when it's idle, and waking it can take up to a minute. Try again shortly.",
          retryable: true,
        }
      default:
        return { kind: "unknown", title: "Something went wrong", message: error.message, retryable: true }
    }
  }
  // fetch() itself failed: offline, or the connection dropped.
  if (error instanceof TypeError) {
    return {
      kind: "network",
      title: "Can't reach the server",
      message: "Check your connection and try again.",
      retryable: true,
    }
  }
  const message = error instanceof Error && error.message ? error.message : "An unexpected error happened."
  return { kind: "unknown", title: "Something went wrong", message, retryable: true }
}

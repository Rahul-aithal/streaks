// Typed client for the Go API. Requests go through the Vite proxy at /api.

export type Source = "manual" | "github"

export interface StreakResult {
  current: number
  longest: number
  last_completed: string | null
}

export interface Habit {
  id: number
  name: string
  source: Source
  target_days: number[]
  archived: boolean
  archived_at: string | null
  created_at: string
  updated_at: string
  streak: StreakResult
}

export interface HistoryDay {
  day: string
  scheduled: boolean
  done: boolean
  contributions?: number
}

export interface HabitHistory {
  habit: Habit
  days: HistoryDay[]
}

export interface SyncRun {
  last_success_at: string | null
  last_error: string | null
}

export interface SummaryHabit {
  id: number
  name: string
  source: Source
  status: "done" | "pending" | "rest"
  contributions?: number
  streak: StreakResult
}

export interface SummaryTask {
  id: string
  title: string
  status: string
  due: string | null
  overdue: boolean
  completed_at: string | null
}

export interface Summary {
  date: string
  habits: SummaryHabit[]
  tasks: {
    total: number
    completed: number
    overdue: number
    items: SummaryTask[]
    sync: SyncRun
  }
  streaks: { daily: StreakResult }
  github: { contributions: number; sync: SyncRun }
}

export interface HabitInput {
  name: string
  source?: Source
  target_days: number[]
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: {
      "X-Streaks-Secret": import.meta.env.VITE_API_SECRET as string,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T

  const data: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const message =
      data && typeof data === "object" && "error" in data && typeof data.error === "string"
        ? data.error
        : `Request failed (${res.status}). Is the API running?`
    throw new ApiError(message, res.status)
  }
  return data as T
}

export const api = {
  summary: () => request<Summary>("GET", "/summary/today"),
  habits: () => request<Habit[]>("GET", "/habits"),
  history: (id: number) => request<HabitHistory>("GET", `/habits/${id}/history`),
  createHabit: (input: HabitInput) => request<Habit>("POST", "/habits", input),
  updateHabit: (id: number, input: Omit<HabitInput, "source">) =>
    request<Habit>("PATCH", `/habits/${id}`, input),
  deleteHabit: (id: number) => request<void>("DELETE", `/habits/${id}`),
  archiveHabit: (id: number) => request<Habit>("POST", `/habits/${id}/archive`),
  unarchiveHabit: (id: number) => request<Habit>("POST", `/habits/${id}/unarchive`),
  checkIn: (id: number, day: string) => request<Habit>("PUT", `/habits/${id}/check-ins/${day}`),
  undoCheckIn: (id: number, day: string) =>
    request<Habit>("DELETE", `/habits/${id}/check-ins/${day}`),
  syncGitHub: () => request<SyncRun>("POST", "/sync/github"),
  syncGoogleTasks: () => request<SyncRun>("POST", "/sync/google-tasks"),
}

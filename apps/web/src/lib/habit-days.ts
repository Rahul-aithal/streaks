import type { Source } from "@/lib/api"

// Quick picks for a habit's target days. Days are 0 (Sun) to 6 (Sat); an empty list
// means every day.
export const DAY_PRESETS = [
  { id: "every", label: "Every day", days: [] as number[] },
  { id: "weekdays", label: "Weekdays", days: [1, 2, 3, 4, 5] },
  { id: "weekends", label: "Weekends", days: [0, 6] },
] as const

export type PresetId = (typeof DAY_PRESETS)[number]["id"]

// Which preset the selected days amount to, if any. All seven days is the same as none.
export function activePreset(days: string[]): PresetId | undefined {
  const picked = days.map(Number).toSorted((a, b) => a - b)
  const normal = picked.length === 7 ? [] : picked
  return DAY_PRESETS.find((p) => p.days.join() === normal.join())?.id
}

// What a form holds, kept so a stray click outside the dialog doesn't lose it.
export interface HabitDraft {
  name: string
  source: Source
  days: string[]
}

const PREFIX = "streaks-habit-draft:"

export const draftKey = (habit?: { id: number }) => PREFIX + (habit ? `edit:${habit.id}` : "new")

const isDraft = (v: unknown): v is HabitDraft =>
  typeof v === "object" &&
  v !== null &&
  "name" in v &&
  typeof v.name === "string" &&
  "source" in v &&
  (v.source === "manual" || v.source === "github") &&
  "days" in v &&
  Array.isArray(v.days) &&
  v.days.every((d) => typeof d === "string")

export function loadDraft(key: string): HabitDraft | null {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(key) ?? "null")
    return isDraft(parsed) ? parsed : null
  } catch {
    return null // Storage can be blocked, and the saved text can be anything.
  }
}

export function saveDraft(key: string, draft: HabitDraft) {
  try {
    localStorage.setItem(key, JSON.stringify(draft))
  } catch {
    // A private window can refuse storage. The form still works; it just won't be kept.
  }
}

export function clearDraft(key: string) {
  try {
    localStorage.removeItem(key)
  } catch {
    // Nothing to clear if storage is blocked.
  }
}

export const sameDraft = (a: HabitDraft, b: HabitDraft) =>
  a.name === b.name && a.source === b.source && a.days.toSorted().join() === b.days.toSorted().join()

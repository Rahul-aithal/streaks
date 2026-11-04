// Days are "YYYY-MM-DD" strings from the API, which owns the IST day boundary.
// Treating them as UTC here just prevents the browser's zone from shifting them.

const parse = (day: string) => new Date(`${day}T00:00:00Z`)

export const weekdayOf = (day: string) => parse(day).getUTCDay()

export const formatDay = (day: string, options: Intl.DateTimeFormatOptions) =>
  parse(day).toLocaleDateString("en-IN", { timeZone: "UTC", ...options })

export const formatLong = (day: string) =>
  formatDay(day, { weekday: "short", day: "numeric", month: "short", year: "numeric" })

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
export const WEEKDAY_OPTIONS = WEEKDAYS.map((label, value) => ({ label, value }))

export function describeTargets(days: number[]): string {
  if (days.length === 7) return "Every day"
  if (days.join() === "1,2,3,4,5") return "Weekdays"
  return days.map((d) => WEEKDAYS[d]).join(", ")
}

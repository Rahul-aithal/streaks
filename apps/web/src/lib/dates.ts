// Days are "YYYY-MM-DD" strings from the API, which owns the IST day boundary.
// Treating them as UTC here just prevents the browser's zone from shifting them.

const parse = (day: string) => new Date(`${day}T00:00:00Z`)

export const addDays = (day: string, n: number) =>
  new Date(parse(day).getTime() + n * 86_400_000).toISOString().slice(0, 10)

// The calendar works in local Date objects; these map to and from the API's day
// strings without the browser's zone moving the day.
export const toLocalDate = (day: string) => {
  const [y = 0, m = 1, d = 1] = day.split("-").map(Number)
  return new Date(y, m - 1, d)
}

export const fromLocalDate = (date: Date) =>
  `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`

export const weekdayOf = (day: string) => parse(day).getUTCDay()

export const formatDay = (day: string, options: Intl.DateTimeFormatOptions) =>
  parse(day).toLocaleDateString("en-IN", { timeZone: "UTC", ...options })

export const formatLong = (day: string) =>
  formatDay(day, { weekday: "short", day: "numeric", month: "short", year: "numeric" })

export const formatInstant = (iso: string) =>
  new Date(iso).toLocaleString("en-IN", {
    timeZone: "Asia/Kolkata",
    dateStyle: "medium",
    timeStyle: "short",
  })

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
export const WEEKDAY_OPTIONS = WEEKDAYS.map((label, value) => ({ label, value }))

export function describeTargets(days: number[]): string {
  if (days.length === 0 || days.length === 7) return "Every day"
  if (days.join() === "1,2,3,4,5") return "Weekdays"
  return days.map((d) => WEEKDAYS[d]).join(", ")
}

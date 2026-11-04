import type { StreakResult } from "@/lib/api"
import { formatDay } from "@/lib/dates"

const plural = (n: number) => `${n} ${n === 1 ? "day" : "days"}`

export function StreakStats({ streak }: { streak: StreakResult }) {
  return (
    <dl className="grid grid-cols-3 gap-4">
      <Stat label="Current" value={plural(streak.current)} />
      <Stat label="Longest" value={plural(streak.longest)} />
      <Stat
        label="Last completed"
        value={
          streak.last_completed ? formatDay(streak.last_completed, { day: "numeric", month: "short" }) : "Never"
        }
      />
    </dl>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-xl font-semibold tabular-nums">{value}</dd>
    </div>
  )
}

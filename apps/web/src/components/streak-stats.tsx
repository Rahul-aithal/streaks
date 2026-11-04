import type { LucideIcon } from "lucide-react"
import { CalendarCheck, Flame, Trophy } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import type { StreakResult } from "@/lib/api"
import { formatDay } from "@/lib/dates"

const plural = (n: number) => `${n} ${n === 1 ? "day" : "days"}`

export function StatTile({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: string }) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className="truncate text-xl font-semibold tabular-nums">{value}</p>
        </div>
      </CardContent>
    </Card>
  )
}

export function StreakStats({ streak }: { streak: StreakResult }) {
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <StatTile icon={Flame} label="Current streak" value={plural(streak.current)} />
      <StatTile icon={Trophy} label="Longest streak" value={plural(streak.longest)} />
      <StatTile
        icon={CalendarCheck}
        label="Last completed"
        value={streak.last_completed ? formatDay(streak.last_completed, { day: "numeric", month: "short" }) : "Never"}
      />
    </div>
  )
}

import { dayCellClass, DAY_LABEL } from "@/components/day-cell"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { DayState, HistoryDay } from "@/lib/api"
import { formatDay, formatLong, weekdayOf } from "@/lib/dates"
import { cn } from "@/lib/utils"

const LEGEND: { state: DayState; label: string }[] = [
  { state: "done", label: "Done" },
  { state: "bonus", label: "Non-target day" },
  { state: "missed", label: "Missed" },
  { state: "pending", label: "Today" },
  { state: "rest", label: "Nothing scheduled" },
]

export function cellState(d: HistoryDay, today: string): DayState {
  if (d.done) return d.scheduled ? "done" : "bonus"
  if (!d.scheduled) return "rest"
  return d.day < today ? "missed" : "pending"
}

const CELL_SIZE = "size-3"
const WEEK_WIDTH = 15 // Cell (12px) plus gap (3px).

export function Heatmap({ days, today }: { days: HistoryDay[]; today: string }) {
  const first = days[0]
  if (!first) return null
  const padding = weekdayOf(first.day)

  // A month label sits above the first week that starts in that month.
  const months: { column: number; label: string }[] = []
  days.forEach((d, i) => {
    if (!d.day.endsWith("-01")) return
    const position = i + padding
    months.push({ column: Math.ceil(position / 7), label: formatDay(d.day, { month: "short" }) })
  })

  return (
    <div className="flex flex-col gap-3">
      <div className="overflow-x-auto pb-1">
        <div className="flex w-max gap-2">
          <div className="grid grid-rows-7 gap-[3px] pt-5 text-[10px] leading-3 text-muted-foreground">
            {["", "Mon", "", "Wed", "", "Fri", ""].map((label, i) => (
              <span key={i}>{label}</span>
            ))}
          </div>
          <div>
            <div className="relative mb-1 h-4 text-[10px] text-muted-foreground">
              {months.map((m) => (
                <span key={`${m.column}-${m.label}`} className="absolute" style={{ left: m.column * WEEK_WIDTH }}>
                  {m.label}
                </span>
              ))}
            </div>
            <div className="grid grid-flow-col grid-rows-7 gap-[3px]">
              {Array.from({ length: padding }, (_, i) => (
                <span key={i} className={CELL_SIZE} />
              ))}
              {days.map((d) => (
                <Cell key={d.day} day={d} state={cellState(d, today)} />
              ))}
            </div>
          </div>
        </div>
      </div>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {LEGEND.map(({ state, label }) => (
          <li key={state} className="flex items-center gap-1.5">
            <span className={cn(CELL_SIZE, dayCellClass(state))} />
            {label}
          </li>
        ))}
      </ul>
    </div>
  )
}

function Cell({ day, state }: { day: HistoryDay; state: DayState }) {
  const contributions =
    day.contributions === undefined
      ? ""
      : ` · ${day.contributions} ${day.contributions === 1 ? "contribution" : "contributions"}`
  const text = `${formatLong(day.day)}: ${DAY_LABEL[state]}${contributions}`
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={-1} aria-label={text} role="img" className={cn(CELL_SIZE, dayCellClass(state))} />
      </TooltipTrigger>
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  )
}

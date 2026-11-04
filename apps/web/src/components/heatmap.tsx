import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { HistoryDay } from "@/lib/api"
import { formatDay, formatLong, weekdayOf } from "@/lib/dates"
import { cn } from "@/lib/utils"

type CellState = "done" | "bonus" | "missed" | "pending" | "rest"

// States differ by shape as well as colour, so they survive colour-blindness.
const CELL: Record<CellState, string> = {
  done: "bg-emerald-700",
  bonus: "bg-emerald-200 ring-1 ring-inset ring-emerald-700",
  missed: "bg-background ring-1 ring-inset ring-rose-500",
  pending: "bg-background ring-2 ring-inset ring-foreground/50",
  rest: "bg-muted",
}

const LABEL: Record<CellState, string> = {
  done: "Done",
  bonus: "Done on a non-target day",
  missed: "Missed",
  pending: "Not done yet",
  rest: "Nothing scheduled",
}

const LEGEND: { state: CellState; label: string }[] = [
  { state: "done", label: "Done" },
  { state: "bonus", label: "Non-target day" },
  { state: "missed", label: "Missed" },
  { state: "pending", label: "Today" },
  { state: "rest", label: "Nothing scheduled" },
]

export function cellState(d: HistoryDay, today: string): CellState {
  if (d.done) return d.scheduled ? "done" : "bonus"
  if (!d.scheduled) return "rest"
  return d.day < today ? "missed" : "pending"
}

export function stateLabel(state: CellState) {
  return LABEL[state]
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
    const column = Math.ceil(position / 7) // The first week starting in this month.
    months.push({ column, label: formatDay(d.day, { month: "short" }) })
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
            <span className={cn(CELL_SIZE, "rounded-[3px]", CELL[state])} />
            {label}
          </li>
        ))}
      </ul>
    </div>
  )
}

function Cell({ day, state }: { day: HistoryDay; state: CellState }) {
  const contributions =
    day.contributions === undefined
      ? ""
      : ` · ${day.contributions} ${day.contributions === 1 ? "contribution" : "contributions"}`
  const text = `${formatLong(day.day)}: ${LABEL[state]}${contributions}`
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={-1} aria-label={text} role="img" className={cn(CELL_SIZE, "rounded-[3px]", CELL[state])} />
      </TooltipTrigger>
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  )
}

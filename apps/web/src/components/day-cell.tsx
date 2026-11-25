import type { DayState, HistoryDay } from "@/lib/api"
import { cn } from "@/lib/utils"

// States differ by shape as well as colour, so they survive colour-blindness.
const CELL: Record<DayState, string> = {
  done: "bg-primary",
  saved: "bg-primary ring-2 ring-inset ring-amber-400",
  bonus: "bg-primary/25 ring-1 ring-inset ring-primary",
  missed: "bg-background ring-1 ring-inset ring-destructive",
  pending: "bg-background ring-2 ring-inset ring-foreground/50",
  cool_off: "bg-sky-400/30 ring-1 ring-inset ring-sky-500",
  rest: "bg-muted",
}

export const DAY_LABEL: Record<DayState, string> = {
  done: "Done",
  saved: "Saved with points",
  bonus: "Done on a non-target day",
  missed: "Missed",
  pending: "Not done yet",
  cool_off: "Cool-off day",
  rest: "Nothing scheduled",
}

export function dayCellClass(state: DayState) {
  return cn("rounded-[3px]", CELL[state])
}

export function cellState(d: HistoryDay, today: string): DayState {
  if (d.saved) return "saved"
  if (d.done) return d.scheduled ? "done" : "bonus"
  if (!d.scheduled) return "rest"
  if (d.cool_off) return "cool_off"
  return d.day < today ? "missed" : "pending"
}

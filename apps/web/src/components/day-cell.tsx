import type { DayState } from "@/lib/api"
import { cn } from "@/lib/utils"

// States differ by shape as well as colour, so they survive colour-blindness.
const CELL: Record<DayState, string> = {
  done: "bg-primary",
  bonus: "bg-primary/25 ring-1 ring-inset ring-primary",
  missed: "bg-background ring-1 ring-inset ring-destructive",
  pending: "bg-background ring-2 ring-inset ring-foreground/50",
  rest: "bg-muted",
}

export const DAY_LABEL: Record<DayState, string> = {
  done: "Done",
  bonus: "Done on a non-target day",
  missed: "Missed",
  pending: "Not done yet",
  rest: "Nothing scheduled",
}

export function dayCellClass(state: DayState) {
  return cn("rounded-[3px]", CELL[state])
}

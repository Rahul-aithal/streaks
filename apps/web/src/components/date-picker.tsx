import { CalendarIcon } from "lucide-react"
import { lazy, Suspense, useState } from "react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { formatLong, fromLocalDate, toLocalDate } from "@/lib/dates"
import { cn } from "@/lib/utils"

// The calendar library is the heaviest part of the page, so it loads only when the
// picker is opened (not on hover), behind a skeleton.
const Calendar = lazy(() => import("@/components/ui/calendar").then((m) => ({ default: m.Calendar })))

// Picks one day, as a "YYYY-MM-DD" string, between min and max (inclusive).
export function DatePicker({
  value,
  onChange,
  min,
  max,
  label,
  className,
}: {
  value: string
  onChange: (day: string) => void
  min: string
  max: string
  label: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          aria-label={`${label}: ${formatLong(value)}`}
          className={cn("h-9 min-w-0 justify-start gap-2 px-3 font-normal tabular-nums", className)}
        >
          <CalendarIcon className="text-muted-foreground" />
          <span className="truncate">{formatLong(value)}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-auto p-0">
        <Suspense fallback={<Skeleton className="m-2 h-60 w-64" />}>
        <Calendar
          mode="single"
          selected={toLocalDate(value)}
          defaultMonth={toLocalDate(value)}
          onSelect={(date) => {
            if (!date) return
            onChange(fromLocalDate(date))
            setOpen(false)
          }}
          disabled={{ before: toLocalDate(min), after: toLocalDate(max) }}
          autoFocus
        />
        </Suspense>
      </PopoverContent>
    </Popover>
  )
}

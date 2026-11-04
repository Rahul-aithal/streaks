import { type FormEvent, type ReactNode, useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api, type Habit, type Source } from "@/lib/api"
import { WEEKDAY_OPTIONS } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

export function HabitFormDialog({ habit, trigger }: { habit?: Habit; trigger: ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent>
        {/* Mounted only while open, so each opening starts from fresh values. */}
        {open && <HabitForm habit={habit} onDone={() => setOpen(false)} />}
      </DialogContent>
    </Dialog>
  )
}

function HabitForm({ habit, onDone }: { habit?: Habit; onDone: () => void }) {
  const [name, setName] = useState(habit?.name ?? "")
  const [source, setSource] = useState<Source>(habit?.source ?? "manual")
  // All seven days and no days both mean "every day"; show that as no selection.
  const [days, setDays] = useState<string[]>(
    habit && habit.target_days.length < 7 ? habit.target_days.map(String) : [],
  )

  const save = useApiMutation(
    () => {
      const target_days = days.map(Number)
      return habit
        ? api.updateHabit(habit.id, { name, target_days })
        : api.createHabit({ name, source, target_days })
    },
    onDone,
  )

  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate(undefined)
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{habit ? "Edit habit" : "New habit"}</DialogTitle>
        <DialogDescription>
          {habit
            ? "Target day changes apply from today. Past streaks keep their old schedule."
            : "Streaks are worked out from your check-ins."}
        </DialogDescription>
      </DialogHeader>

      <div className="flex flex-col gap-2">
        <Label htmlFor="habit-name">Name</Label>
        <Input
          id="habit-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={100}
          required
          autoFocus
        />
      </div>

      {!habit && (
        <div className="flex flex-col gap-2">
          <Label>Source</Label>
          <ToggleGroup
            type="single"
            variant="outline"
            value={source}
            onValueChange={(v) => v && setSource(v as Source)}
            className="justify-start"
          >
            <ToggleGroupItem value="manual">Manual</ToggleGroupItem>
            <ToggleGroupItem value="github">GitHub</ToggleGroupItem>
          </ToggleGroup>
          <p className="text-xs text-muted-foreground">
            {source === "github"
              ? "Checked in automatically on days with a push or an opened PR."
              : "You check it in yourself."}
          </p>
        </div>
      )}

      <div className="flex flex-col gap-2">
        <Label>Target days</Label>
        <ToggleGroup
          type="multiple"
          variant="outline"
          value={days}
          onValueChange={setDays}
          className="flex-wrap justify-start"
        >
          {WEEKDAY_OPTIONS.map((d) => (
            <ToggleGroupItem key={d.value} value={String(d.value)}>
              {d.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <p className="text-xs text-muted-foreground">
          {days.length === 0 ? "None selected means every day." : "Other days hold the streak."}
        </p>
      </div>

      <DialogFooter>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

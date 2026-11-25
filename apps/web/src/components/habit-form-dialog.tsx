import { useQuery } from "@tanstack/react-query"
import { Check, GitBranch, Hand, RotateCcw } from "lucide-react"
import { type FormEvent, type ReactNode, useEffect, useState } from "react"
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
import { describeTargets, WEEKDAY_OPTIONS } from "@/lib/dates"
import {
  activePreset,
  clearDraft,
  DAY_PRESETS,
  draftKey,
  type HabitDraft,
  loadDraft,
  sameDraft,
  saveDraft,
} from "@/lib/habit-days"
import { useApiMutation } from "@/lib/use-api-mutation"
import { cn } from "@/lib/utils"

const NAME_LIMIT = 100
const ALL_DAYS = WEEKDAY_OPTIONS.map((d) => String(d.value))
const SUGGESTIONS = ["Read 20 pages", "Workout", "Meditate", "Study", "Write"]

export function HabitFormDialog({
  habit,
  trigger,
  defaultOpen = false,
}: {
  habit?: Habit
  trigger: ReactNode
  defaultOpen?: boolean
}) {
  const [open, setOpen] = useState(defaultOpen)
  const key = draftKey(habit)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent
        className="sm:max-w-md"
        // A click that lands outside while there's unsaved input is almost always a
        // slip. Escape and the close button still close it, and keep the draft.
        onInteractOutside={(e) => {
          if (loadDraft(key)) e.preventDefault()
        }}
      >
        {/* Mounted only while open, so each opening starts from the saved draft. */}
        {open && <HabitForm habit={habit} draftId={key} onDone={() => setOpen(false)} />}
      </DialogContent>
    </Dialog>
  )
}

function initialOf(habit?: Habit): HabitDraft {
  return {
    name: habit?.name ?? "",
    source: habit?.source ?? "manual",
    // All seven days and no days both mean "every day"; that's shown as no selection.
    days: habit && habit.target_days.length < 7 ? habit.target_days.map(String) : [],
  }
}

function HabitForm({ habit, draftId, onDone }: { habit?: Habit; draftId: string; onDone: () => void }) {
  const initial = initialOf(habit)
  // What was typed last time, if the dialog was closed before saving.
  const [restored] = useState(() => {
    const draft = loadDraft(draftId)
    return draft && !sameDraft(draft, initial) ? draft : null
  })
  const start = restored ?? initial
  const [name, setName] = useState(start.name)
  const [source, setSource] = useState<Source>(start.source)
  const [days, setDays] = useState<string[]>(start.days)
  const [showRestored, setShowRestored] = useState(restored !== null)

  // Only one active GitHub activity habit is allowed: it already counts every contribution.
  const habits = useQuery({ queryKey: ["habits"], queryFn: api.habits, enabled: !habit })
  const githubHabit = habits.data?.find((h) => h.source === "github" && !h.archived)

  // Keep what's typed until it's saved, so closing by mistake loses nothing.
  useEffect(() => {
    const current = { name, source, days }
    if (sameDraft(current, initial)) clearDraft(draftId)
    else saveDraft(draftId, current)
    // `initial` is rebuilt each render from the same habit; the fields are what change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [name, source, days, draftId])

  const reset = () => {
    setName(initial.name)
    setSource(initial.source)
    setDays(initial.days)
    setShowRestored(false)
    clearDraft(draftId)
  }

  const save = useApiMutation(
    () => {
      const target_days = days.map(Number)
      const trimmed = name.trim()
      return habit
        ? api.updateHabit(habit.id, { name: trimmed, target_days })
        : api.createHabit({ name: trimmed, source, target_days })
    },
    () => {
      clearDraft(draftId)
      onDone()
    },
  )

  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate(undefined)
  }

  const preset = activePreset(days)
  const canSave = name.trim() !== "" && !save.isPending

  return (
    <form onSubmit={submit} className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle>{habit ? "Edit habit" : "New habit"}</DialogTitle>
        <DialogDescription>
          {habit
            ? "Target day changes apply from today. Past streaks keep their old schedule."
            : "Pick something small you can do on the days you choose."}
        </DialogDescription>
      </DialogHeader>

      {showRestored && (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-xs">
          <span>We kept what you had typed.</span>
          <Button type="button" variant="ghost" size="xs" onClick={reset}>
            <RotateCcw />
            Start over
          </Button>
        </div>
      )}

      <div className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between">
          <Label htmlFor="habit-name">Name</Label>
          {name.length >= NAME_LIMIT - 20 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {name.length}/{NAME_LIMIT}
            </span>
          )}
        </div>
        <Input
          id="habit-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={NAME_LIMIT}
          placeholder="e.g. Read 20 pages"
          autoComplete="off"
          required
          autoFocus
        />
        {!habit && name === "" && (
          <div className="flex flex-wrap gap-1.5">
            {SUGGESTIONS.map((s) => (
              <Button key={s} type="button" variant="outline" size="xs" onClick={() => setName(s)}>
                {s}
              </Button>
            ))}
          </div>
        )}
      </div>

      {!habit && (
        <div className="flex flex-col gap-2">
          <Label id="habit-source">Tracked by</Label>
          <ToggleGroup
            type="single"
            variant="outline"
            aria-labelledby="habit-source"
            value={source}
            onValueChange={(v) => v && setSource(v as Source)}
            className="grid w-full grid-cols-2 gap-2"
          >
            <SourceOption value="manual" icon={<Hand />} title="You" note="Check it in yourself" />
            <SourceOption
              value="github"
              icon={<GitBranch />}
              title="GitHub"
              note={githubHabit ? "You already have one" : "Counts your contributions"}
              disabled={githubHabit !== undefined}
            />
          </ToggleGroup>
          {source === "github" && (
            <p className="text-xs text-muted-foreground">
              Checked in on days with a contribution (the green squares on your profile). Connect GitHub in Settings.
            </p>
          )}
        </div>
      )}

      <div className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between gap-2">
          <Label id="habit-days">Repeats</Label>
          <span className="text-xs text-muted-foreground">{describeTargets(days.map(Number).toSorted((a, b) => a - b))}</span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {DAY_PRESETS.map((p) => (
            <Button
              key={p.id}
              type="button"
              size="xs"
              variant={preset === p.id ? "default" : "outline"}
              aria-pressed={preset === p.id}
              onClick={() => setDays(p.days.map(String))}
            >
              {p.label}
            </Button>
          ))}
        </div>
        <ToggleGroup
          type="multiple"
          variant="outline"
          aria-labelledby="habit-days"
          // No days and all seven mean the same thing, so "every day" lights up all of them.
          value={days.length === 0 ? ALL_DAYS : days}
          onValueChange={(next) => setDays(next.length === 7 ? [] : next)}
          className="grid w-full grid-cols-7 gap-1.5"
        >
          {WEEKDAY_OPTIONS.map((d) => (
            <ToggleGroupItem
              key={d.value}
              value={String(d.value)}
              aria-label={d.label}
              className="h-9 w-full min-w-0 px-0 text-xs data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
            >
              {d.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        {days.length > 0 && <p className="text-xs text-muted-foreground">On the other days your streak holds.</p>}
      </div>

      <DialogFooter>
        <Button type="submit" disabled={!canSave}>
          {save.isPending ? "Saving…" : habit ? "Save changes" : "Create habit"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function SourceOption({
  value,
  icon,
  title,
  note,
  disabled,
}: {
  value: Source
  icon: ReactNode
  title: string
  note: string
  disabled?: boolean
}) {
  return (
    <ToggleGroupItem
      value={value}
      disabled={disabled}
      className={cn(
        "group/option relative h-auto w-full flex-col items-start gap-0.5 rounded-lg px-3 py-2.5 text-left whitespace-normal text-foreground",
        // The toggle's own "on" look is a solid brand fill with dark text. A card reads
        // better as a tinted panel, so put the text colour back to normal on top of it.
        "data-[state=on]:border-primary data-[state=on]:bg-primary/15 data-[state=on]:text-foreground data-[state=on]:hover:bg-primary/20 data-[state=on]:hover:text-foreground",
        "disabled:opacity-100 disabled:text-muted-foreground",
      )}
    >
      <span className="flex items-center gap-1.5 font-medium">
        <span className="text-muted-foreground group-data-[state=on]/option:text-primary">{icon}</span>
        {title}
      </span>
      <span className="text-xs font-normal text-muted-foreground group-data-[state=on]/option:text-foreground/80">{note}</span>
      <Check className="absolute top-2.5 right-2.5 size-4 text-primary opacity-0 group-data-[state=on]/option:opacity-100" />
    </ToggleGroupItem>
  )
}

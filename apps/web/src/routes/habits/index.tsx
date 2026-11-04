import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { Plus } from "lucide-react"
import { ConfirmButton } from "@/components/confirm-button"
import { HabitFormDialog } from "@/components/habit-form-dialog"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Habit } from "@/lib/api"
import { describeTargets } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

export const Route = createFileRoute("/habits/")({ component: HabitsPage })

function HabitsPage() {
  const habits = useQuery({ queryKey: ["habits"], queryFn: api.habits })

  return (
    <>
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold">Habits</h1>
        <HabitFormDialog
          trigger={
            <Button size="sm">
              <Plus />
              New habit
            </Button>
          }
        />
      </div>
      {habits.isPending && <Skeleton className="h-40" />}
      {habits.isError && <QueryError error={habits.error} onRetry={() => void habits.refetch()} />}
      {habits.data && <HabitLists habits={habits.data} />}
    </>
  )
}

function HabitLists({ habits }: { habits: Habit[] }) {
  const active = habits.filter((h) => !h.archived)
  const archived = habits.filter((h) => h.archived)
  return (
    <>
      {active.length === 0 && <p className="text-sm text-muted-foreground">No active habits.</p>}
      <ul className="flex flex-col gap-2">
        {active.map((habit) => (
          <HabitItem key={habit.id} habit={habit} />
        ))}
      </ul>
      {archived.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-sm font-medium text-muted-foreground">Archived</h2>
          <ul className="flex flex-col gap-2">
            {archived.map((habit) => (
              <HabitItem key={habit.id} habit={habit} />
            ))}
          </ul>
        </section>
      )}
    </>
  )
}

function HabitItem({ habit }: { habit: Habit }) {
  const archive = useApiMutation(() => api.archiveHabit(habit.id))
  const unarchive = useApiMutation(() => api.unarchiveHabit(habit.id))

  return (
    <li className="flex items-center justify-between gap-3 rounded-lg border p-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <Link
            to="/habits/$id"
            params={{ id: String(habit.id) }}
            className="truncate font-medium hover:underline"
          >
            {habit.name}
          </Link>
          {habit.source === "github" && <Badge variant="secondary">GitHub</Badge>}
        </div>
        <p className="text-xs text-muted-foreground tabular-nums">
          {describeTargets(habit.target_days)} · {habit.streak.current} day streak · longest {habit.streak.longest}
        </p>
      </div>
      {habit.archived ? (
        <Button variant="outline" size="sm" disabled={unarchive.isPending} onClick={() => unarchive.mutate(undefined)}>
          Unarchive
        </Button>
      ) : (
        <ConfirmButton
          title={`Archive "${habit.name}"?`}
          description="It stops being scheduled and leaves the Today view. Its history is kept, and you can unarchive it later."
          confirmLabel="Archive"
          disabled={archive.isPending}
          onConfirm={() => archive.mutate(undefined)}
        >
          Archive
        </ConfirmButton>
      )}
    </li>
  )
}

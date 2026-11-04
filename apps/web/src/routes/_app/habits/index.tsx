import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { Flame, GitBranch, Plus } from "lucide-react"
import { ConfirmButton } from "@/components/confirm-button"
import { HabitFormDialog } from "@/components/habit-form-dialog"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Habit } from "@/lib/api"
import { describeTargets } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

export const Route = createFileRoute("/_app/habits/")({ component: HabitsPage })

function HabitsPage() {
  const habits = useQuery({ queryKey: ["habits"], queryFn: api.habits })

  return (
    <>
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">Habits</h1>
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
      {active.length === 0 && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-muted-foreground">
            No active habits. Create one to start a streak.
          </CardContent>
        </Card>
      )}
      <ul className="grid gap-4 sm:grid-cols-2">
        {active.map((habit) => (
          <HabitItem key={habit.id} habit={habit} />
        ))}
      </ul>
      {archived.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="text-sm font-medium text-muted-foreground">Archived</h2>
          <ul className="grid gap-4 opacity-75 sm:grid-cols-2">
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
    <li>
      <Card className="h-full">
        <CardContent className="flex h-full flex-col gap-3">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <Link
                to="/habits/$id"
                params={{ id: String(habit.id) }}
                className="block truncate font-medium hover:underline"
              >
                {habit.name}
              </Link>
              <p className="text-xs text-muted-foreground">{describeTargets(habit.target_days)}</p>
            </div>
            {habit.source === "github" && (
              <Badge variant="secondary">
                <GitBranch />
                GitHub
              </Badge>
            )}
          </div>
          <div className="mt-auto flex items-center justify-between gap-2">
            <p className="flex items-center gap-1.5 text-sm tabular-nums">
              <Flame className="size-4 text-primary" />
              <span className="font-semibold">{habit.streak.current}</span>
              <span className="text-muted-foreground">· best {habit.streak.longest}</span>
            </p>
            {habit.archived ? (
              <Button variant="ghost" size="sm" disabled={unarchive.isPending} onClick={() => unarchive.mutate(undefined)}>
                Unarchive
              </Button>
            ) : (
              <ConfirmButton
                title={`Archive "${habit.name}"?`}
                description="It stops being scheduled and leaves the Today view. Its history is kept, and you can unarchive it later."
                confirmLabel="Archive"
                variant="ghost"
                disabled={archive.isPending}
                onConfirm={() => archive.mutate(undefined)}
              >
                Archive
              </ConfirmButton>
            )}
          </div>
        </CardContent>
      </Card>
    </li>
  )
}

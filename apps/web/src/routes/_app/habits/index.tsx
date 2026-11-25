import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link, useRouter } from "@tanstack/react-router"
import { Archive, ArchiveRestore, Flame, GitBranch, Plus } from "lucide-react"
import { ConfirmButton } from "@/components/confirm-button"
import { HabitFormButton } from "@/components/habit-form-button"
import { MoreList } from "@/components/more-list"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Habit } from "@/lib/api"
import { describeTargets } from "@/lib/dates"
import { habitsQuery } from "@/lib/queries"
import { useApiMutation } from "@/lib/use-api-mutation"

export const Route = createFileRoute("/_app/habits/")({
  loader: ({ context }) => {
    if (typeof window !== "undefined") void context.queryClient.prefetchQuery(habitsQuery)
  },
  component: HabitsPage,
})

const GRID = "grid gap-3 sm:grid-cols-2 lg:grid-cols-3"

function HabitsPage() {
  const habits = useQuery(habitsQuery)
  const active = habits.data?.filter((h) => !h.archived) ?? []

  return (
    <>
      <div className="flex items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Habits</h1>
          <p className="text-sm text-muted-foreground">
            {habits.data ? `${active.length} active` : " "}
          </p>
        </div>
        <HabitFormButton size="sm">
          <Plus />
          New habit
        </HabitFormButton>
      </div>
      {habits.isPending && (
        <div className={GRID} aria-busy>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      )}
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
      {active.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-10 text-center">
            <span className="flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
              <Flame className="size-6" />
            </span>
            <p className="text-sm text-muted-foreground">No active habits. Create one to start a streak.</p>
          </CardContent>
        </Card>
      ) : (
        <ul className={GRID}>
          {active.map((habit) => (
            <HabitItem key={habit.id} habit={habit} />
          ))}
          <li>
            {/* Fills the empty slot in the grid and invites the next habit. */}
            <HabitFormButton
              variant="outline"
              className="h-full min-h-[70px] w-full flex-col gap-1 rounded-xl border-dashed text-muted-foreground hover:border-primary hover:text-primary"
            >
              <Plus className="size-5" />
              New habit
            </HabitFormButton>
          </li>
        </ul>
      )}
      {archived.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="text-sm font-medium text-muted-foreground">Archived · {archived.length}</h2>
          {/* A few show; the rest open on click. */}
          <MoreList
            items={archived}
            limit={3}
            listClassName={GRID}
            render={(habit) => <HabitItem key={habit.id} habit={habit} />}
          />
        </section>
      )}
    </>
  )
}

// "3-day streak" in the brand colour, or a quiet "No streak" when there isn't one.
function StreakBadge({ streak }: { streak: number }) {
  return streak > 0 ? (
    <Badge variant="secondary" className="gap-1 tabular-nums">
      <Flame className="text-primary" />
      {streak}-day streak
    </Badge>
  ) : (
    <Badge variant="outline" className="text-muted-foreground">
      No streak
    </Badge>
  )
}

function HabitItem({ habit }: { habit: Habit }) {
  const router = useRouter()
  // Hovering (or tabbing to, or touching) anywhere on the card starts loading the
  // habit's page code and its history request. The heatmap is the heavy part, so it
  // waits until the page is actually open.
  const warm = () => void router.preloadRoute({ to: "/habits/$id", params: { id: String(habit.id) } })
  const archive = useApiMutation(() => api.archiveHabit(habit.id))
  const unarchive = useApiMutation(() => api.unarchiveHabit(habit.id))

  return (
    <li>
      <Card
        size="sm"
        className={habit.archived ? "h-full opacity-80" : "h-full"}
        onPointerEnter={warm}
        onFocus={warm}
        onTouchStart={warm}
      >
        <CardContent className="flex h-full items-start gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Link
              to="/habits/$id"
              params={{ id: String(habit.id) }}
              preload="intent"
              className="block truncate font-medium hover:underline"
            >
              {habit.name}
            </Link>
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <StreakBadge streak={habit.streak.current} />
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {habit.source === "github" && <GitBranch className="size-3 shrink-0" aria-label="GitHub activity" />}
                {describeTargets(habit.target_days)}
                {habit.streak.longest > 0 && ` · best ${habit.streak.longest}`}
              </p>
            </div>
          </div>
          {habit.archived ? (
            <Button
              variant="outline"
              size="icon-sm"
              aria-label={`Unarchive ${habit.name}`}
              title="Unarchive"
              disabled={unarchive.isPending}
              onClick={() => unarchive.mutate(undefined)}
            >
              <ArchiveRestore />
            </Button>
          ) : (
            <ConfirmButton
              title={`Archive "${habit.name}"?`}
              description="It stops being scheduled and leaves the Today view. Its history is kept, and you can unarchive it later."
              confirmLabel="Archive"
              size="icon-sm"
              label={`Archive ${habit.name}`}
              disabled={archive.isPending}
              onConfirm={() => archive.mutate(undefined)}
            >
              <Archive />
            </ConfirmButton>
          )}
        </CardContent>
      </Card>
    </li>
  )
}

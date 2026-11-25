import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { ArrowLeft, Check, GitBranch, LifeBuoy, Snowflake } from "lucide-react"
import { lazy, Suspense } from "react"
import { ConfirmButton } from "@/components/confirm-button"
import { cellState, DAY_LABEL } from "@/components/day-cell"
import { HabitFormButton } from "@/components/habit-form-button"
import { QueryError } from "@/components/query-error"
import { StreakStats } from "@/components/streak-stats"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Habit, type HistoryDay } from "@/lib/api"
import { describeTargets, formatLong } from "@/lib/dates"
import { historyQuery, rewardsQuery } from "@/lib/queries"
import { useApiMutation } from "@/lib/use-api-mutation"

// The heatmap and its tooltips load as their own chunk, after the header and stats.
const Heatmap = lazy(() => import("@/components/heatmap").then((m) => ({ default: m.Heatmap })))

export const Route = createFileRoute("/_app/habits/$id")({
  loader: ({ context, params }) => {
    if (typeof window !== "undefined") void context.queryClient.prefetchQuery(historyQuery(Number(params.id)))
  },
  component: HabitPage,
})

const RECENT_DAYS = 7
const BACKDATE_DAYS = 3 // Today plus the 2 days the API allows.

function HabitPage() {
  const id = Number(Route.useParams().id)
  const history = useQuery(historyQuery(id))
  const days = history.data?.days
  const today = days?.at(-1)?.day ?? ""
  // Points only matter when a recent day can be saved, so only then ask for them.
  const canBuySave = Boolean(
    history.data &&
      !history.data.habit.archived &&
      days?.slice(-RECENT_DAYS).some((d) => cellState(d, today) === "missed" || history.data.habit.source === "github"),
  )
  const rewards = useQuery({ ...rewardsQuery, enabled: canBuySave })

  if (history.isPending) {
    return (
      <div className="flex flex-col gap-5" aria-busy>
        <Skeleton className="h-16" />
        <Skeleton className="h-16" />
        <Skeleton className="h-48" />
      </div>
    )
  }
  if (history.isError) return <QueryError error={history.error} onRetry={() => void history.refetch()} />

  const { habit } = history.data
  return (
    <>
      <Header habit={habit} />
      <StreakStats streak={habit.streak} />
      <Card size="sm">
        <CardHeader>
          <CardTitle>History</CardTitle>
        </CardHeader>
        <CardContent>
          <Suspense fallback={<Skeleton className="h-36" />}>
            <Heatmap days={history.data.days} today={today} />
          </Suspense>
        </CardContent>
      </Card>
      <Card size="sm">
        <CardHeader>
          <CardTitle>Recent days</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="grid gap-x-8 md:grid-cols-2">
            {history.data.days
              .slice(-RECENT_DAYS)
              .toReversed()
              .map((day, index) => (
                <RecentDay
                  key={day.day}
                  habit={habit}
                  day={day}
                  today={today}
                  canCheckIn={index < BACKDATE_DAYS && habit.source === "manual" && !habit.archived}
                  saveCost={rewards.data?.costs.habit_save}
                  balance={rewards.data?.wallet.balance ?? 0}
                />
              ))}
          </ul>
        </CardContent>
      </Card>
    </>
  )
}

function Header({ habit }: { habit: Habit }) {
  const navigate = useNavigate()
  const archive = useApiMutation(() => api.archiveHabit(habit.id))
  const unarchive = useApiMutation(() => api.unarchiveHabit(habit.id))
  const remove = useApiMutation(
    () => api.deleteHabit(habit.id),
    () => void navigate({ to: "/habits" }),
  )

  return (
    <div className="flex flex-col gap-2">
      <Link to="/habits" className="flex w-fit items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4" />
        Habits
      </Link>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="flex flex-wrap items-center gap-2 text-2xl font-semibold tracking-tight">
            <span className="break-words">{habit.name}</span>
            {habit.source === "github" && (
              <Badge variant="secondary">
                <GitBranch />
                GitHub
              </Badge>
            )}
            {habit.archived && <Badge variant="outline">Archived</Badge>}
          </h1>
          <p className="text-sm text-muted-foreground">{describeTargets(habit.target_days)}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {!habit.archived && (
            <HabitFormButton habit={habit} variant="outline" size="sm">
              Edit
            </HabitFormButton>
          )}
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
          <ConfirmButton
            title={`Delete "${habit.name}"?`}
            description="This permanently removes the habit and all its check-ins, and can change your daily streak. Archive it instead to keep the history."
            confirmLabel="Delete"
            variant="destructive"
            disabled={remove.isPending}
            onConfirm={() => remove.mutate(undefined)}
          >
            Delete
          </ConfirmButton>
        </div>
      </div>
    </div>
  )
}

function RecentDay({
  habit,
  day,
  today,
  canCheckIn,
  saveCost,
  balance,
}: {
  habit: Habit
  day: HistoryDay
  today: string
  canCheckIn: boolean
  saveCost: number | undefined
  balance: number
}) {
  const toggle = useApiMutation(() =>
    day.done ? api.undoCheckIn(habit.id, day.day) : api.checkIn(habit.id, day.day),
  )
  const save = useApiMutation(() => api.saveStreak(habit.id, day.day))
  // A missed day can be bought back with points. Today is still open, so only a
  // GitHub habit (which can't be checked in by hand) can be saved today.
  const canSave =
    saveCost !== undefined &&
    !habit.archived &&
    cellState(day, today) === (day.day < today ? "missed" : "pending") &&
    (day.day < today || habit.source === "github")
  return (
    <li className="flex min-h-11 items-center justify-between gap-3 border-b py-1.5 text-sm last:border-b-0 md:[&:nth-last-child(2)]:border-b-0">
      <span>{formatLong(day.day)}</span>
      <span className="flex items-center gap-3 text-muted-foreground">
        {day.contributions !== undefined && (
          <span className="tabular-nums">
            {day.contributions} {day.contributions === 1 ? "contribution" : "contributions"}
          </span>
        )}
        {canSave && (
          <Button
            size="sm"
            variant="secondary"
            disabled={save.isPending || balance < (saveCost ?? 0)}
            title={balance < (saveCost ?? 0) ? "Finish Google Tasks to earn points" : undefined}
            onClick={() => save.mutate(undefined)}
          >
            <LifeBuoy className="text-amber-500" />
            Save · {saveCost} pts
          </Button>
        )}
        {day.saved ? (
          <span className="flex items-center gap-1">
            <LifeBuoy className="size-4 text-amber-500" />
            Saved with points
          </span>
        ) : day.cool_off && !day.done ? (
          <span className="flex items-center gap-1">
            <Snowflake className="size-4 text-sky-500" />
            Cool-off day
          </span>
        ) : canCheckIn ? (
          <Button
            size="sm"
            variant={day.done ? "default" : "outline"}
            disabled={toggle.isPending}
            aria-pressed={day.done}
            onClick={() => toggle.mutate(undefined)}
          >
            <Check />
            {day.done ? "Done" : "Check in"}
          </Button>
        ) : (
          <span>{DAY_LABEL[cellState(day, today)]}</span>
        )}
      </span>
    </li>
  )
}

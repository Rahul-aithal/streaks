import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { ArrowLeft, Check, GitBranch } from "lucide-react"
import { ConfirmButton } from "@/components/confirm-button"
import { DAY_LABEL } from "@/components/day-cell"
import { HabitFormDialog } from "@/components/habit-form-dialog"
import { cellState, Heatmap } from "@/components/heatmap"
import { QueryError } from "@/components/query-error"
import { StreakStats } from "@/components/streak-stats"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Habit, type HistoryDay } from "@/lib/api"
import { describeTargets, formatLong } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

export const Route = createFileRoute("/_app/habits/$id")({ component: HabitPage })

const RECENT_DAYS = 7
const BACKDATE_DAYS = 3 // Today plus the 2 days the API allows.

function HabitPage() {
  const id = Number(Route.useParams().id)
  const history = useQuery({ queryKey: ["history", id], queryFn: () => api.history(id) })

  if (history.isPending) return <Skeleton className="h-64" />
  if (history.isError) return <QueryError error={history.error} onRetry={() => void history.refetch()} />

  const { habit, days } = history.data
  const today = days.at(-1)?.day ?? ""
  return (
    <>
      <Header habit={habit} />
      <StreakStats streak={habit.streak} />
      <Card>
        <CardHeader>
          <CardTitle>History</CardTitle>
        </CardHeader>
        <CardContent>
          <Heatmap days={days} today={today} />
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Recent days</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="flex flex-col divide-y">
            {days
              .slice(-RECENT_DAYS)
              .toReversed()
              .map((day, index) => (
                <RecentDay
                  key={day.day}
                  habit={habit}
                  day={day}
                  today={today}
                  canCheckIn={index < BACKDATE_DAYS && habit.source === "manual" && !habit.archived}
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
            <HabitFormDialog
              habit={habit}
              trigger={
                <Button variant="outline" size="sm">
                  Edit
                </Button>
              }
            />
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
}: {
  habit: Habit
  day: HistoryDay
  today: string
  canCheckIn: boolean
}) {
  const toggle = useApiMutation(() =>
    day.done ? api.undoCheckIn(habit.id, day.day) : api.checkIn(habit.id, day.day),
  )
  return (
    <li className="flex min-h-11 items-center justify-between gap-3 py-1.5 text-sm">
      <span>{formatLong(day.day)}</span>
      <span className="flex items-center gap-3 text-muted-foreground">
        {day.contributions !== undefined && (
          <span className="tabular-nums">
            {day.contributions} {day.contributions === 1 ? "contribution" : "contributions"}
          </span>
        )}
        {canCheckIn ? (
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

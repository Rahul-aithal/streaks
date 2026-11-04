import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { Check, GitBranch, RefreshCw } from "lucide-react"
import { QueryError } from "@/components/query-error"
import { StreakStats } from "@/components/streak-stats"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Summary, type SummaryHabit, type SyncRun } from "@/lib/api"
import { formatDay, formatLong } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

export const Route = createFileRoute("/")({ component: TodayPage })

function TodayPage() {
  const summary = useQuery({ queryKey: ["summary"], queryFn: api.summary })

  if (summary.isPending) return <TodaySkeleton />
  if (summary.isError) return <QueryError error={summary.error} onRetry={() => void summary.refetch()} />

  const { data } = summary
  return (
    <>
      <h1 className="text-2xl font-semibold">{formatLong(data.date)}</h1>
      <Card>
        <CardHeader>
          <CardTitle>Daily completion streak</CardTitle>
        </CardHeader>
        <CardContent>
          <StreakStats streak={data.streaks.daily} />
        </CardContent>
      </Card>
      <HabitsCard summary={data} />
      <TasksCard summary={data} />
    </>
  )
}

function TodaySkeleton() {
  return (
    <div className="flex flex-col gap-6" aria-busy>
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-32" />
      <Skeleton className="h-48" />
      <p className="text-xs text-muted-foreground">The first load after a while can take a few seconds.</p>
    </div>
  )
}

function HabitsCard({ summary }: { summary: Summary }) {
  const syncGitHub = useApiMutation(api.syncGitHub)
  const hasGitHubHabit = summary.habits.some((h) => h.source === "github")

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <CardTitle>Habits</CardTitle>
        {hasGitHubHabit && (
          <Button variant="outline" size="sm" disabled={syncGitHub.isPending} onClick={() => syncGitHub.mutate(undefined)}>
            <RefreshCw className={syncGitHub.isPending ? "animate-spin" : ""} />
            Sync GitHub
          </Button>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {summary.habits.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No habits yet.{" "}
            <Link to="/habits" className="underline">
              Create one
            </Link>
            .
          </p>
        )}
        {summary.habits.map((habit) => (
          <HabitRow key={habit.id} habit={habit} day={summary.date} />
        ))}
        {hasGitHubHabit && <SyncNote label="GitHub" run={summary.github.sync} />}
      </CardContent>
    </Card>
  )
}

function HabitRow({ habit, day }: { habit: SummaryHabit; day: string }) {
  const toggle = useApiMutation(() =>
    habit.status === "done" ? api.undoCheckIn(habit.id, day) : api.checkIn(habit.id, day),
  )
  const isDone = habit.status === "done"

  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
      <div className="min-w-0">
        <Link
          to="/habits/$id"
          params={{ id: String(habit.id) }}
          className="block truncate font-medium hover:underline"
        >
          {habit.name}
        </Link>
        <p className="text-xs text-muted-foreground tabular-nums">
          {habit.streak.current} day streak · longest {habit.streak.longest}
          {habit.contributions !== undefined && ` · ${habit.contributions} today`}
        </p>
      </div>
      {habit.source === "github" ? (
        <Badge variant={isDone ? "default" : "outline"}>
          {isDone ? <Check /> : <GitBranch />}
          {isDone ? "Done" : habit.status === "rest" ? "Rest day" : "Waiting for activity"}
        </Badge>
      ) : (
        <div className="flex shrink-0 items-center gap-2">
          {habit.status === "rest" && <span className="text-xs text-muted-foreground">Rest day</span>}
          <Button
            size="sm"
            variant={isDone ? "default" : "outline"}
            disabled={toggle.isPending}
            aria-pressed={isDone}
            onClick={() => toggle.mutate(undefined)}
          >
            <Check />
            {isDone ? "Done" : "Check in"}
          </Button>
        </div>
      )}
    </div>
  )
}

function TasksCard({ summary }: { summary: Summary }) {
  const { tasks } = summary
  const sync = useApiMutation(api.syncGoogleTasks)

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <CardTitle>Tasks</CardTitle>
        <Button variant="outline" size="sm" disabled={sync.isPending} onClick={() => sync.mutate(undefined)}>
          <RefreshCw className={sync.isPending ? "animate-spin" : ""} />
          Sync tasks
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm tabular-nums">
          {tasks.completed} of {tasks.total} done · {tasks.overdue} overdue
        </p>
        {tasks.items.length === 0 ? (
          <p className="text-sm text-muted-foreground">Nothing due today.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {tasks.items.map((task) => {
              const done = task.status === "completed"
              return (
                <li key={task.id} className="flex items-center justify-between gap-3 text-sm">
                  <span className={done ? "text-muted-foreground line-through" : ""}>{task.title}</span>
                  {task.overdue && task.due && (
                    <Badge variant="destructive">Due {formatDay(task.due, { day: "numeric", month: "short" })}</Badge>
                  )}
                </li>
              )
            })}
          </ul>
        )}
        <SyncNote label="Google Tasks" run={tasks.sync} />
      </CardContent>
    </Card>
  )
}

function SyncNote({ label, run }: { label: string; run: SyncRun }) {
  const when = run.last_success_at
    ? new Date(run.last_success_at).toLocaleString("en-IN", {
        timeZone: "Asia/Kolkata",
        dateStyle: "medium",
        timeStyle: "short",
      })
    : null
  return (
    <div className="text-xs text-muted-foreground">
      <p>{when ? `${label} last synced ${when}` : `${label} hasn't synced yet`}</p>
      {run.last_error && <p className="break-words text-destructive">Last sync failed: {run.last_error}</p>}
    </div>
  )
}

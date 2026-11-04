import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { Check, CircleCheck, Flame, GitBranch, ListTodo, Plus, RefreshCw } from "lucide-react"
import { dayCellClass, DAY_LABEL } from "@/components/day-cell"
import { GoogleSignInButton } from "@/components/google-button"
import { QueryError } from "@/components/query-error"
import { ProgressRing } from "@/components/progress-ring"
import { StatTile } from "@/components/streak-stats"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api, GOOGLE_RECONNECT_URL, type Summary, type SummaryHabit, type SyncRun } from "@/lib/api"
import { formatDay, formatInstant, formatLong } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"
import { firstName, useMe } from "@/lib/use-me"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/")({ component: TodayPage })

function greeting() {
  const hour = Number(
    new Intl.DateTimeFormat("en-IN", { timeZone: "Asia/Kolkata", hour: "numeric", hourCycle: "h23" }).format(new Date()),
  )
  if (hour < 12) return "Good morning"
  return hour < 17 ? "Good afternoon" : "Good evening"
}

function TodayPage() {
  const summary = useQuery({ queryKey: ["summary"], queryFn: api.summary })
  const { active } = useMe()

  return (
    <>
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          {greeting()}, {firstName(active)}
        </h1>
        <p className="text-sm text-muted-foreground">{summary.data ? formatLong(summary.data.date) : " "}</p>
      </div>
      {summary.isPending && (
        <div className="flex flex-col gap-6" aria-busy>
          <Skeleton className="h-24" />
          <Skeleton className="h-64" />
        </div>
      )}
      {summary.isError && <QueryError error={summary.error} onRetry={() => void summary.refetch()} />}
      {summary.data && <TodayContent summary={summary.data} />}
    </>
  )
}

function TodayContent({ summary }: { summary: Summary }) {
  const scheduled = summary.habits.filter((h) => h.status !== "rest")
  const done = scheduled.filter((h) => h.status === "done").length
  const { daily } = summary.streaks

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-3">
        <Card>
          <CardContent className="flex items-center gap-4">
            <ProgressRing done={done} total={scheduled.length} />
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">Today</p>
              <p className="font-semibold">
                {scheduled.length === 0
                  ? "Nothing scheduled"
                  : done === scheduled.length
                    ? "All done!"
                    : `${scheduled.length - done} to go`}
              </p>
            </div>
          </CardContent>
        </Card>
        <StatTile icon={Flame} label="Daily streak" value={`${daily.current} ${daily.current === 1 ? "day" : "days"}`} />
        <StatTile
          icon={ListTodo}
          label="Tasks today"
          value={summary.tasks.total === 0 ? "None" : `${summary.tasks.completed} of ${summary.tasks.total}`}
        />
      </div>
      <div className="grid items-start gap-6 lg:grid-cols-3">
        <HabitsCard summary={summary} className="lg:col-span-2" />
        <TasksCard summary={summary} />
      </div>
    </>
  )
}

function HabitsCard({ summary, className }: { summary: Summary; className?: string }) {
  const syncGitHub = useApiMutation(api.syncGitHub)
  const hasGitHubHabit = summary.habits.some((h) => h.source === "github")

  return (
    <Card className={className}>
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <CardTitle>Habits</CardTitle>
        {hasGitHubHabit && summary.github.linked && (
          <Button variant="outline" size="sm" disabled={syncGitHub.isPending} onClick={() => syncGitHub.mutate(undefined)}>
            <RefreshCw className={syncGitHub.isPending ? "animate-spin" : ""} />
            Sync GitHub
          </Button>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {summary.habits.length === 0 && (
          <div className="flex flex-col items-center gap-3 py-8 text-center">
            <span className="flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
              <Flame className="size-6" />
            </span>
            <div>
              <p className="font-medium">Start your first streak</p>
              <p className="text-sm text-muted-foreground">Add a habit to track it here every day.</p>
            </div>
            <Button size="sm" asChild>
              <Link to="/habits">
                <Plus />
                Create a habit
              </Link>
            </Button>
          </div>
        )}
        {summary.habits.map((habit) => (
          <HabitRow key={habit.id} habit={habit} day={summary.date} />
        ))}
        {hasGitHubHabit && !summary.github.linked && (
          <p className="text-sm text-muted-foreground">
            <Link to="/settings" className="underline">
              Connect your GitHub account
            </Link>{" "}
            so GitHub habits complete automatically.
          </p>
        )}
        {hasGitHubHabit && summary.github.linked && <SyncNote label="GitHub" run={summary.github.sync} />}
      </CardContent>
    </Card>
  )
}

function HabitRow({ habit, day }: { habit: SummaryHabit; day: string }) {
  const toggle = useApiMutation(() =>
    habit.status === "done" ? api.undoCheckIn(habit.id, day) : api.checkIn(habit.id, day),
  )
  // Show the click right away: a Neon round trip shouldn't feel like lag.
  const isDone = toggle.isPending ? habit.status !== "done" : habit.status === "done"
  const isGitHub = habit.source === "github"

  return (
    <div className="flex items-center gap-3 rounded-xl border bg-card p-3 transition-colors">
      {isGitHub ? (
        <span
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-full border",
            isDone ? "border-primary bg-primary text-primary-foreground" : "text-muted-foreground",
          )}
          title={isDone ? "Completed by GitHub activity" : "Completes automatically on GitHub activity"}
        >
          {isDone ? <Check className="size-5" /> : <GitBranch className="size-4" />}
        </span>
      ) : (
        <button
          type="button"
          aria-pressed={isDone}
          aria-label={isDone ? `Undo check-in for ${habit.name}` : `Check in ${habit.name}`}
          disabled={toggle.isPending}
          onClick={() => toggle.mutate(undefined)}
          className={cn(
            "flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-full border-2 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring",
            isDone
              ? "border-primary bg-primary text-primary-foreground"
              : "border-muted-foreground/30 text-transparent hover:border-primary hover:text-primary/50",
          )}
        >
          <Check className="size-5" />
        </button>
      )}
      <div className="min-w-0 flex-1">
        <Link
          to="/habits/$id"
          params={{ id: String(habit.id) }}
          className={cn("block truncate font-medium hover:underline", isDone && "text-muted-foreground")}
        >
          {habit.name}
        </Link>
        <p className="text-xs text-muted-foreground tabular-nums">
          {habit.status === "rest" && !isDone && "Rest day · "}
          Best {habit.streak.longest}
          {habit.contributions !== undefined &&
            ` · ${habit.contributions} ${habit.contributions === 1 ? "contribution" : "contributions"} today`}
        </p>
      </div>
      <div className="hidden items-center gap-1 sm:flex">
        {habit.recent.map((d) => (
          <Tooltip key={d.day}>
            <TooltipTrigger asChild>
              <span
                role="img"
                aria-label={`${formatLong(d.day)}: ${DAY_LABEL[d.state]}`}
                className={cn("size-3.5", dayCellClass(d.state))}
              />
            </TooltipTrigger>
            <TooltipContent>
              {formatDay(d.day, { weekday: "short", day: "numeric", month: "short" })}: {DAY_LABEL[d.state]}
            </TooltipContent>
          </Tooltip>
        ))}
      </div>
      <Badge variant={habit.streak.current > 0 ? "secondary" : "outline"} className="shrink-0 tabular-nums">
        <Flame className={habit.streak.current > 0 ? "text-primary" : ""} />
        {habit.streak.current}
      </Badge>
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
        <Button
          variant="ghost"
          size="icon"
          aria-label="Sync Google Tasks"
          disabled={sync.isPending}
          onClick={() => sync.mutate(undefined)}
        >
          <RefreshCw className={sync.isPending ? "animate-spin" : ""} />
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {tasks.needs_reconnect && (
          <div className="flex flex-col gap-2 rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
            <p>Google access expired, so these tasks may be out of date.</p>
            <GoogleSignInButton href={GOOGLE_RECONNECT_URL} size="sm">
              Reconnect Google
            </GoogleSignInButton>
          </div>
        )}
        {tasks.overdue > 0 && (
          <p className="text-sm text-destructive tabular-nums">{tasks.overdue} overdue</p>
        )}
        {tasks.items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-6 text-center text-sm text-muted-foreground">
            <CircleCheck className="size-6" />
            Nothing due today.
          </div>
        ) : (
          <ul className="flex flex-col gap-2">
            {tasks.items.map((task) => {
              const done = task.status === "completed"
              return (
                <li key={task.id} className="flex items-start gap-2 text-sm">
                  <span
                    className={cn(
                      "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border",
                      done && "border-primary bg-primary text-primary-foreground",
                    )}
                  >
                    {done && <Check className="size-3" />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className={done ? "text-muted-foreground line-through" : ""}>{task.title}</span>
                    {task.overdue && task.due && (
                      <span className="block text-xs text-destructive">
                        Due {formatDay(task.due, { day: "numeric", month: "short" })}
                      </span>
                    )}
                  </span>
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
  return (
    <div className="text-xs text-muted-foreground">
      <p>{run.last_success_at ? `${label} synced ${formatInstant(run.last_success_at)}` : `${label} hasn't synced yet`}</p>
      {run.last_error && <p className="break-words text-destructive">Last sync failed: {run.last_error}</p>}
    </div>
  )
}

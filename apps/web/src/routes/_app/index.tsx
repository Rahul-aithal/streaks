import { useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { lazy, Suspense, useState } from "react"
import { Check, CircleCheck, Flame, GitBranch, ListTodo, Plus, RefreshCw, Snowflake, Sparkles } from "lucide-react"
import { dayCellClass, DAY_LABEL } from "@/components/day-cell"
import { GoogleSignInButton } from "@/components/google-button"
import { MoreList } from "@/components/more-list"
import { QueryError } from "@/components/query-error"
import { RewardsSkeleton } from "@/components/rewards-skeleton"
import { ProgressRing } from "@/components/progress-ring"
import { StatTile } from "@/components/streak-stats"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api, GOOGLE_RECONNECT_URL, type Summary, type SummaryHabit, type SummaryTask, type SyncRun } from "@/lib/api"
import { formatDay, formatInstant, formatLong } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"
import { rewardsQuery, summaryQuery } from "@/lib/queries"
import { firstName, useMe } from "@/lib/use-me"
import { cn } from "@/lib/utils"

// The panel and its data load when the Rewards tab is first reached.
const loadRewards = () => import("@/components/rewards-panel")
const RewardsPanel = lazy(loadRewards)

export const Route = createFileRoute("/_app/")({
  // Starts the request with the navigation instead of after the page renders, in
  // parallel with the sign-in check. Not awaited: the page shows its own skeleton.
  loader: ({ context }) => {
    if (typeof window !== "undefined") void context.queryClient.prefetchQuery(summaryQuery)
  },
  component: TodayPage,
})

function greeting() {
  const hour = Number(
    new Intl.DateTimeFormat("en-IN", { timeZone: "Asia/Kolkata", hour: "numeric", hourCycle: "h23" }).format(new Date()),
  )
  if (hour < 12) return "Good morning"
  return hour < 17 ? "Good afternoon" : "Good evening"
}

function TodayPage() {
  const summary = useQuery(summaryQuery)
  const { active } = useMe()

  return (
    <>
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          {greeting()}, {firstName(active)}
        </h1>
        <p className="text-sm text-muted-foreground">{summary.data ? formatLong(summary.data.date) : " "}</p>
      </div>
      {summary.isPending && <TodaySkeleton />}
      {summary.isError && <QueryError error={summary.error} onRetry={() => void summary.refetch()} />}
      {summary.data && <TodayContent summary={summary.data} />}
    </>
  )
}

function TodaySkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-busy>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-[72px]" />
        ))}
      </div>
      <div className="grid gap-5 lg:grid-cols-5">
        <Skeleton className="h-72 lg:col-span-3" />
        <Skeleton className="h-72 lg:col-span-2" />
      </div>
    </div>
  )
}

function TodayContent({ summary }: { summary: Summary }) {
  // A cool-off day takes a habit off today's list, like a rest day.
  const scheduled = summary.habits.filter((h) => h.status === "done" || h.status === "pending")
  const done = scheduled.filter((h) => h.status === "done").length
  const { daily } = summary.streaks

  return (
    <>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Card size="sm">
          <CardContent className="flex items-center gap-3">
            <ProgressRing done={done} total={scheduled.length} />
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">Today</p>
              <p className="truncate font-semibold">
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
        <StatTile
          icon={Sparkles}
          label="Reward points"
          value={`${summary.rewards.balance} ${summary.rewards.balance === 1 ? "pt" : "pts"}`}
        />
      </div>
      {/* Habits on the left; tasks and rewards share one card on the right, so the
          page is one screen instead of three stacked cards. */}
      <div className="grid items-start gap-5 lg:grid-cols-5">
        <HabitsCard summary={summary} className="lg:col-span-3" />
        <SideCard summary={summary} />
      </div>
    </>
  )
}

function SideCard({ summary }: { summary: Summary }) {
  const [tab, setTab] = useState("tasks")
  const queryClient = useQueryClient()
  const openTasks = summary.tasks.items.length + summary.tasks.later.length
  // Hovering or focusing the tab is enough warning to fetch the code and data.
  const warmRewards = () => {
    void loadRewards()
    void queryClient.prefetchQuery(rewardsQuery)
  }

  return (
    <Card className="lg:col-span-2">
      <Tabs value={tab} onValueChange={setTab} className="gap-4">
        <CardHeader>
          <TabsList className="w-full">
            <TabsTrigger value="tasks">
              <ListTodo />
              Tasks
              {openTasks > 0 && <span className="text-xs text-muted-foreground tabular-nums">{openTasks}</span>}
            </TabsTrigger>
            <TabsTrigger value="rewards" onPointerEnter={warmRewards} onFocus={warmRewards}>
              <Sparkles />
              Rewards
              <span className="text-xs text-muted-foreground tabular-nums">{summary.rewards.balance}</span>
            </TabsTrigger>
          </TabsList>
        </CardHeader>
        <CardContent>
          <TabsContent value="tasks">
            <TasksPanel summary={summary} />
          </TabsContent>
          <TabsContent value="rewards">
            <Suspense fallback={<RewardsSkeleton />}>
              <RewardsPanel summary={summary} />
            </Suspense>
          </TabsContent>
        </CardContent>
      </Tabs>
    </Card>
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
            isDone ? "border-primary bg-primary text-primary-foreground" : "border-control text-muted-foreground",
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
              : "border-control text-transparent hover:border-primary hover:text-primary/50",
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
          {habit.status === "cool_off" && !isDone && (
            <>
              <Snowflake className="inline size-3 text-sky-500" /> Cool-off day ·{" "}
            </>
          )}
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

function TasksPanel({ summary }: { summary: Summary }) {
  const { tasks } = summary
  // An API that hasn't been redeployed yet won't send this; treat it as empty.
  const later = tasks.later ?? []
  const sync = useApiMutation(api.syncGoogleTasks)

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <p className={cn("text-sm tabular-nums", tasks.overdue > 0 ? "font-medium text-destructive" : "text-muted-foreground")}>
          {tasks.overdue > 0 ? `${tasks.overdue} overdue` : "Google Tasks"}
        </p>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Sync Google Tasks"
          disabled={sync.isPending}
          onClick={() => sync.mutate(undefined)}
        >
          <RefreshCw className={sync.isPending ? "animate-spin" : ""} />
        </Button>
      </div>
      {tasks.needs_reconnect && (
        <div className="flex flex-col gap-2 rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
          <p>Google access expired, so these tasks may be out of date.</p>
          <GoogleSignInButton href={GOOGLE_RECONNECT_URL} size="sm">
            Reconnect Google
          </GoogleSignInButton>
        </div>
      )}
      {tasks.read_only && !tasks.needs_reconnect && (
        <div className="flex flex-col gap-2 rounded-lg border border-border bg-muted/50 p-3 text-sm">
          <p className="text-muted-foreground">
            Streaks can see your tasks but can&apos;t tick them off yet. Reconnect Google and allow it to manage
            your tasks.
          </p>
          <GoogleSignInButton href={GOOGLE_RECONNECT_URL} size="sm">
            Reconnect Google
          </GoogleSignInButton>
        </div>
      )}
      {tasks.items.length === 0 && later.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-6 text-center text-sm text-muted-foreground">
          <CircleCheck className="size-6" />
          No open tasks.
        </div>
      ) : (
        <>
          {tasks.items.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing due today.</p>
          ) : (
            <TaskList tasks={tasks.items} />
          )}
          {later.length > 0 && (
            <section className="flex flex-col gap-2 border-t pt-3">
              <h3 className="text-xs font-medium text-muted-foreground">Upcoming &amp; no date</h3>
              <TaskList tasks={later} limit={LATER_LIMIT} upcoming />
            </section>
          )}
        </>
      )}
      <SyncNote label="Google Tasks" run={tasks.sync} />
    </div>
  )
}

const LATER_LIMIT = 4

function TaskList({ tasks, limit, upcoming }: { tasks: SummaryTask[]; limit?: number; upcoming?: boolean }) {
  return (
    <MoreList
      items={tasks}
      limit={limit ?? tasks.length}
      render={(task) => <TaskItem key={task.id} task={task} showDue={upcoming || task.overdue} />}
    />
  )
}

function TaskItem({ task, showDue }: { task: SummaryTask; showDue: boolean }) {
  const name = task.title || "Untitled task"
  const toggle = useApiMutation(() => (task.status === "completed" ? api.reopenTask(task.id) : api.completeTask(task.id)))
  // Show the tap right away, like habits do; it settles when the refetch lands.
  const done = toggle.isPending ? task.status !== "completed" : task.status === "completed"
  return (
    <li className="flex items-start gap-2.5 text-sm">
      <button
        type="button"
        aria-label={done ? `Mark ${name} as not done` : `Mark ${name} as done`}
        aria-pressed={done}
        disabled={toggle.isPending}
        onClick={() => toggle.mutate(undefined)}
        className={cn(
          "group/check mt-px flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-full border-2 transition-all outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-wait",
          done
            ? "border-primary bg-primary text-primary-foreground"
            : "border-control text-transparent hover:border-primary hover:text-primary/60",
        )}
      >
        <Check className="size-3 transition-transform group-active/check:scale-90" strokeWidth={3} />
      </button>
      <span className="min-w-0 flex-1">
        <span className={cn(done && "text-muted-foreground line-through", !task.title && "text-muted-foreground italic")}>
          {task.title || "Untitled task"}
        </span>
        {task.due && !done && showDue && (
          <span className={cn("block text-xs", task.overdue ? "text-destructive" : "text-muted-foreground")}>
            Due {formatDay(task.due, { day: "numeric", month: "short" })}
          </span>
        )}
      </span>
    </li>
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

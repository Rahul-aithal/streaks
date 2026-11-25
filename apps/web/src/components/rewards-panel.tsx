import { useQuery } from "@tanstack/react-query"
import { LifeBuoy, Snowflake, X } from "lucide-react"
import { useState } from "react"
import { DatePicker } from "@/components/date-picker"
import { MoreList } from "@/components/more-list"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { RewardsSkeleton } from "@/components/rewards-skeleton"
import { api, type RewardEvent, type Rewards, type Summary } from "@/lib/api"
import { rewardsQuery } from "@/lib/queries"
import { addDays, formatDay } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

const shortDay = (day: string) => formatDay(day, { weekday: "short", day: "numeric", month: "short" })
const pts = (n: number) => `${n} ${Math.abs(n) === 1 ? "pt" : "pts"}`

// Finished tasks earn points; points buy a cool-off day (every streak is held)
// or a save (one habit counts as done on a day it was missed). Loaded on demand.
export default function RewardsPanel({ summary }: { summary: Summary }) {
  const rewards = useQuery(rewardsQuery)

  return (
    <div className="flex flex-col gap-3">
      {rewards.isPending && <RewardsSkeleton />}
      {rewards.isError && <QueryError error={rewards.error} onRetry={() => void rewards.refetch()} />}
      {rewards.data && <RewardsContent rewards={rewards.data} summary={summary} />}
    </div>
  )
}

function missedDays(summary: Summary, window: number) {
  const oldest = addDays(summary.date, -window)
  return summary.habits.flatMap((h) =>
    h.recent.filter((d) => d.state === "missed" && d.day >= oldest).map((d) => ({ habit: h, day: d.day })),
  )
}

// Collapsed, this is a progress bar and three rows. Each section opens on click.
function RewardsContent({ rewards, summary }: { rewards: Rewards; summary: Summary }) {
  const { wallet, costs, daily_cap } = rewards
  const missed = missedDays(summary, rewards.window.back)
  // Open straight to the saves when there's something to save and points to do it.
  const startOpen = missed.length > 0 && wallet.balance >= costs.habit_save ? "save" : undefined

  return (
    <>
      <div className="flex flex-col gap-1.5">
        <div className="flex items-baseline justify-between gap-2 text-sm">
          <span className="text-muted-foreground">Earned today</span>
          <span className="font-medium tabular-nums">
            {wallet.earned_today} / {daily_cap}
          </span>
        </div>
        <div className="h-1.5 overflow-hidden rounded-full bg-muted ring-1 ring-inset ring-border" aria-hidden>
          <div
            className="h-full rounded-full bg-amber-400 transition-all"
            style={{ width: `${Math.min(100, (wallet.earned_today / daily_cap) * 100)}%` }}
          />
        </div>
        <p className="text-xs text-muted-foreground">Each finished Google Task earns 1 pt.</p>
      </div>
      <Accordion type="single" collapsible defaultValue={startOpen} className="rounded-lg border px-3">
        <AccordionItem value="cooloff">
          <Row icon={<Snowflake className="size-4 text-sky-500" />} title="Cool-off day" hint={pts(costs.cool_off)} />
          <AccordionContent>
            <CoolOff rewards={rewards} today={summary.date} balance={wallet.balance} />
          </AccordionContent>
        </AccordionItem>
        <AccordionItem value="save">
          <Row
            icon={<LifeBuoy className="size-4 text-amber-500" />}
            title="Save a streak"
            hint={missed.length > 0 ? `${missed.length} missed · ${pts(costs.habit_save)}` : pts(costs.habit_save)}
          />
          <AccordionContent>
            <Saves missed={missed} cost={costs.habit_save} balance={wallet.balance} window={rewards.window.back} />
          </AccordionContent>
        </AccordionItem>
        {rewards.history.length > 0 && (
          <AccordionItem value="history">
            <Row title="Recent points" hint={String(rewards.history.length)} />
            <AccordionContent>
              <History events={rewards.history} />
            </AccordionContent>
          </AccordionItem>
        )}
      </Accordion>
    </>
  )
}

function Row({ icon, title, hint }: { icon?: React.ReactNode; title: string; hint: string }) {
  return (
    <AccordionTrigger className="items-center py-3 hover:no-underline">
      <span className="flex items-center gap-2">
        {icon}
        {title}
      </span>
      <span className="mr-2 ml-auto text-xs font-normal text-muted-foreground tabular-nums">{hint}</span>
    </AccordionTrigger>
  )
}

function CoolOff({ rewards, today, balance }: { rewards: Rewards; today: string; balance: number }) {
  const cost = rewards.costs.cool_off
  const min = addDays(today, -rewards.window.back)
  const max = addDays(today, rewards.window.ahead)
  const [day, setDay] = useState(() => addDays(today, 1))
  const book = useApiMutation(api.bookCoolOff)
  const cancel = useApiMutation(api.cancelCoolOff)
  const booked = rewards.cool_offs.some((c) => c.day === day)
  const valid = day >= min && day <= max

  return (
    <div className="flex flex-col gap-2.5 pb-1">
      <p className="text-xs text-muted-foreground">
        Skip a day without breaking a streak. Book up to {rewards.window.ahead} days ahead, or {rewards.window.back} back.
      </p>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          book.mutate(day)
        }}
      >
        <DatePicker label="Cool-off day" value={day} min={min} max={max} onChange={setDay} className="flex-1" />
        <Button type="submit" size="lg" disabled={book.isPending || !valid || booked || balance < cost}>
          {booked ? "Booked" : "Book"}
        </Button>
      </form>
      {balance < cost && <p className="text-xs text-muted-foreground">You need {pts(cost - balance)} more.</p>}
      {rewards.cool_offs.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {rewards.cool_offs.map((c) => (
            <li key={c.day}>
              <Badge variant="outline" className="gap-1 border-sky-500/50">
                <Snowflake className="text-sky-500" />
                {c.day === today ? "Today" : shortDay(c.day)}
                {c.refundable && (
                  <button
                    type="button"
                    aria-label={`Cancel the cool-off day on ${shortDay(c.day)} and get ${pts(cost)} back`}
                    title="Cancel and refund"
                    disabled={cancel.isPending}
                    onClick={() => cancel.mutate(c.day)}
                    className="-mr-1 cursor-pointer rounded-sm opacity-60 hover:opacity-100"
                  >
                    <X className="size-3" />
                  </button>
                )}
              </Badge>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

type Missed = ReturnType<typeof missedDays>

function Saves({ missed, cost, balance, window }: { missed: Missed; cost: number; balance: number; window: number }) {
  const save = useApiMutation((v: { id: number; day: string }) => api.saveStreak(v.id, v.day))

  if (missed.length === 0) {
    return (
      <p className="pb-1 text-xs text-muted-foreground">
        Nothing missed in the last {window} days. A missed day can be counted as done here.
      </p>
    )
  }
  return (
    <div className="flex flex-col gap-2 pb-1">
      {balance < cost && <p className="text-xs text-muted-foreground">You need {pts(cost - balance)} more to save a streak.</p>}
      <MoreList
        items={missed}
        limit={3}
        listClassName="flex flex-col gap-1.5"
        render={({ habit, day }) => (
          <li key={`${habit.id}-${day}`} className="flex items-center justify-between gap-3 rounded-md border bg-card py-1 pr-1 pl-2.5 text-sm">
            <span className="min-w-0 truncate">
              <span className="font-medium">{habit.name}</span>
              <span className="text-muted-foreground"> · {shortDay(day)}</span>
            </span>
            <Button
              size="sm"
              variant="secondary"
              className="shrink-0"
              disabled={save.isPending || balance < cost}
              title={balance < cost ? `You need ${pts(cost - balance)} more` : undefined}
              onClick={() => save.mutate({ id: habit.id, day })}
            >
              <LifeBuoy />
              Save
            </Button>
          </li>
        )}
      />
    </div>
  )
}

const EVENT_LABEL: Record<RewardEvent["kind"], string> = {
  earned: "Task done",
  cool_off: "Cool-off day",
  habit_save: "Saved",
}

function History({ events }: { events: RewardEvent[] }) {
  return (
    <ul className="flex flex-col gap-1.5 pb-1">
      {events.map((e, i) => (
        <li key={i} className="flex items-center justify-between gap-2 text-xs">
          <span className="min-w-0 truncate">
            {EVENT_LABEL[e.kind]}
            {e.title && `: ${e.title}`} <span className="text-muted-foreground">· {shortDay(e.day)}</span>
          </span>
          <span
            className={e.points > 0 ? "text-primary tabular-nums" : "text-muted-foreground tabular-nums"}
            title={e.kind === "earned" && e.points === 0 ? "Over the daily cap" : undefined}
          >
            {e.points > 0 ? `+${e.points}` : e.points}
          </span>
        </li>
      ))}
    </ul>
  )
}

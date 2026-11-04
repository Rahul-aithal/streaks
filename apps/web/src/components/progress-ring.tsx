const SIZE = 64
const STROKE = 7
const RADIUS = (SIZE - STROKE) / 2
const CIRCUMFERENCE = 2 * Math.PI * RADIUS

export function ProgressRing({ done, total }: { done: number; total: number }) {
  const fraction = total === 0 ? 0 : Math.min(done / total, 1)
  return (
    <div className="relative size-16 shrink-0" role="img" aria-label={`${done} of ${total} habits done today`}>
      <svg viewBox={`0 0 ${SIZE} ${SIZE}`} className="size-16 -rotate-90">
        <circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} fill="none" strokeWidth={STROKE} className="stroke-muted" />
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={RADIUS}
          fill="none"
          strokeWidth={STROKE}
          strokeLinecap="round"
          strokeDasharray={CIRCUMFERENCE}
          strokeDashoffset={CIRCUMFERENCE * (1 - fraction)}
          className="stroke-primary transition-[stroke-dashoffset] duration-500"
        />
      </svg>
      <span className="absolute inset-0 flex items-center justify-center text-sm font-semibold tabular-nums">
        {done}/{total}
      </span>
    </div>
  )
}

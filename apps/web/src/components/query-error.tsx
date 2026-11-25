import { RefreshCw, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { describeError } from "@/lib/errors"

// A failed request inside a page or card: says what happened and offers a retry.
export function QueryError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const info = describeError(error)
  return (
    <div role="alert" className="flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm">
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{info.title}</p>
        <p className="text-muted-foreground">{info.message}</p>
      </div>
      {info.retryable && (
        <Button variant="outline" size="sm" className="shrink-0" onClick={onRetry}>
          <RefreshCw />
          Retry
        </Button>
      )}
    </div>
  )
}

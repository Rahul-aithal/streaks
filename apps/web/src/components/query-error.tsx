import { Button } from "@/components/ui/button"

export function QueryError({ error, onRetry }: { error: Error; onRetry: () => void }) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border p-4 text-sm">
      <span>{error.message}</span>
      <Button variant="outline" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  )
}

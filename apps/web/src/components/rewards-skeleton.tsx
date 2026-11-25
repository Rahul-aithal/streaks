import { Skeleton } from "@/components/ui/skeleton"

// Kept apart from the panel so the page can show it while the panel's code loads.
export function RewardsSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy>
      <Skeleton className="h-12" />
      <Skeleton className="h-28" />
      <Skeleton className="h-24" />
    </div>
  )
}

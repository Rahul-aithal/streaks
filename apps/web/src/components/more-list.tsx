import { ChevronDown } from "lucide-react"
import { type ReactNode, useState } from "react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"

// Shows the first `limit` items; the rest open and close with a height animation.
export function MoreList<T>({
  items,
  limit,
  render,
  listClassName = "flex flex-col gap-2.5",
}: {
  items: T[]
  limit: number
  render: (item: T) => ReactNode
  listClassName?: string // Lays out each part of the list.
}) {
  const [open, setOpen] = useState(false)
  const rest = items.slice(limit)
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <ul className={listClassName}>{items.slice(0, limit).map(render)}</ul>
      {rest.length > 0 && (
        <>
          <CollapsibleContent className="overflow-hidden data-[state=closed]:animate-collapsible-up data-[state=open]:animate-collapsible-down">
            <ul className={cn(listClassName, "pt-3")}>{rest.map(render)}</ul>
          </CollapsibleContent>
          <CollapsibleTrigger asChild>
            <Button variant="outline" size="sm" className="group/more mt-3 w-full text-muted-foreground">
              {open ? "Show less" : `Show ${rest.length} more`}
              <ChevronDown className="transition-transform duration-200 group-data-[state=open]/more:rotate-180" />
            </Button>
          </CollapsibleTrigger>
        </>
      )}
    </Collapsible>
  )
}

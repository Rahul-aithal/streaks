import * as React from "react"
import { cn } from "cn"

// Solid background, one border, and a single soft ring on focus: the border takes
// the brand colour and the ring is a faint halo of it, so the two don't fight.
function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "h-10 w-full min-w-0 rounded-lg border border-input bg-background px-3 text-sm leading-none shadow-xs transition-[border-color,box-shadow] outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground/70 hover:border-foreground/30 focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-primary/20 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-muted disabled:opacity-60 aria-invalid:border-destructive aria-invalid:ring-[3px] aria-invalid:ring-destructive/20",
        className
      )}
      {...props}
    />
  )
}

export { Input }

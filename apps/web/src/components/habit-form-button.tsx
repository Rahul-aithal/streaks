import { type ComponentProps, lazy, type ReactNode, Suspense, useState } from "react"
import { Button } from "@/components/ui/button"
import type { Habit } from "@/lib/api"

const load = () => import("@/components/habit-form-dialog")
const HabitFormDialog = lazy(() => load().then((m) => ({ default: m.HabitFormDialog })))

type ButtonProps = Pick<ComponentProps<typeof Button>, "variant" | "size" | "className">

// A button that opens the habit form. The form's code and its dependencies load on
// hover or focus, so by the click it's usually there; until then it's just a button.
export function HabitFormButton({ habit, children, ...style }: ButtonProps & { habit?: Habit; children: ReactNode }) {
  const [wanted, setWanted] = useState(false)
  const button = (
    <Button
      {...style}
      onPointerEnter={() => void load()}
      onFocus={() => void load()}
      onClick={wanted ? undefined : () => setWanted(true)}
    >
      {children}
    </Button>
  )
  if (!wanted) return button
  return (
    <Suspense fallback={button}>
      <HabitFormDialog habit={habit} defaultOpen trigger={button} />
    </Suspense>
  )
}

import { useEffect, useState } from "react"
import { Logo } from "@/components/logo"

// After a few seconds of waiting, say why: the free API host sleeps when idle.
export function SlowHint({ after = 4000 }: { after?: number }) {
  const [late, setLate] = useState(false)
  useEffect(() => {
    const t = setTimeout(() => setLate(true), after)
    return () => clearTimeout(t)
  }, [after])
  if (!late) return null
  return (
    <p className="page-enter text-center text-xs text-muted-foreground" role="status">
      Waking the server. Free hosting sleeps when it's idle, so the first load can take up to a minute.
    </p>
  )
}

// Full-screen wait: just the logo, beating.
export function LoadingScreen() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 p-6" aria-busy role="status" aria-label="Loading">
      <div className="animate-breathe">
        <Logo className="text-lg" />
      </div>
      <SlowHint />
    </div>
  )
}

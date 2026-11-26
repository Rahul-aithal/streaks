import { Link } from "@tanstack/react-router"
import { Compass, LockKeyhole, RefreshCw, ServerCrash, ShieldAlert, TriangleAlert, WifiOff } from "lucide-react"
import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { describeError, type ErrorKind } from "@/lib/errors"
import { cn } from "@/lib/utils"

const ICON: Record<ErrorKind, typeof Compass> = {
  unauthorized: LockKeyhole,
  forbidden: ShieldAlert,
  not_found: Compass,
  server: ServerCrash,
  network: WifiOff,
  unknown: TriangleAlert,
}

// One look for every dead end: 404, sign-in needed, no access, server down, crash.
export function StatusPage({
  kind,
  title,
  message,
  children,
  fill = true,
}: {
  kind: ErrorKind
  title: string
  message: string
  children?: ReactNode // The actions.
  fill?: boolean // Take the whole screen, or fill the page's remaining height.
}) {
  const Icon = ICON[kind]
  return (
    <div
      className={cn("page-enter flex items-center justify-center p-6", fill ? "min-h-svh" : "flex-1")}
      role={kind === "not_found" ? undefined : "alert"}
    >
      <div className="flex max-w-sm flex-col items-center gap-4 text-center">
        <span className="flex size-16 items-center justify-center rounded-2xl border bg-card shadow-card">
          <Icon className="size-7 text-primary" />
        </span>
        <div className="flex flex-col gap-1.5">
          <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
          <p className="text-sm text-muted-foreground">{message}</p>
        </div>
        {children && <div className="flex flex-wrap justify-center gap-2">{children}</div>}
      </div>
    </div>
  )
}

export function NotFoundPage({ fill }: { fill?: boolean }) {
  return (
    <StatusPage kind="not_found" title="Page not found" message="That page doesn't exist, or it moved." fill={fill}>
      <Button asChild>
        <Link to="/">Go to Today</Link>
      </Button>
    </StatusPage>
  )
}

// Picks the wording and the actions from the error itself.
export function ErrorPage({ error, onRetry, fill }: { error: unknown; onRetry?: () => void; fill?: boolean }) {
  const info = describeError(error)
  return (
    <StatusPage kind={info.kind} title={info.title} message={info.message} fill={fill}>
      {info.kind === "unauthorized" && (
        <Button asChild>
          <Link to="/login" search={{}}>
            Sign in
          </Link>
        </Button>
      )}
      {info.retryable && onRetry && (
        <Button onClick={onRetry}>
          <RefreshCw />
          Try again
        </Button>
      )}
      {info.kind !== "unauthorized" && (
        <Button variant="outline" asChild>
          <Link to="/">Go to Today</Link>
        </Button>
      )}
    </StatusPage>
  )
}

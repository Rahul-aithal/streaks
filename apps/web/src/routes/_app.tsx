import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link, Navigate, Outlet } from "@tanstack/react-router"
import { AccountMenu } from "@/components/account-menu"
import { Logo } from "@/components/logo"
import { QueryError } from "@/components/query-error"
import { ThemeToggle } from "@/components/theme-toggle"
import { Skeleton } from "@/components/ui/skeleton"
import { ApiError } from "@/lib/api"
import { meQuery } from "@/lib/use-me"

export const Route = createFileRoute("/_app")({ component: AppLayout })

const navLink = "rounded-md px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
const navLinkActive = { className: "bg-muted font-medium !text-foreground" }

function AppLayout() {
  const me = useQuery(meQuery)

  if (me.isError && me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" search={{}} replace />
  }
  if (me.isError) {
    return (
      <div className="mx-auto max-w-lg p-6">
        <QueryError error={me.error} onRetry={() => void me.refetch()} />
      </div>
    )
  }
  if (me.isPending) {
    return (
      <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6" aria-busy>
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-32" />
        <Skeleton className="h-64" />
        <p className="text-xs text-muted-foreground">The first load after a while can take a few seconds.</p>
      </div>
    )
  }

  return (
    <div className="flex min-h-svh flex-col">
      <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-2 px-4">
          <Link to="/" className="mr-2 sm:mr-4" aria-label="Streaks home">
            <Logo />
          </Link>
          <nav className="flex gap-1">
            <Link to="/" className={navLink} activeProps={navLinkActive} activeOptions={{ exact: true }}>
              Today
            </Link>
            <Link to="/habits" className={navLink} activeProps={navLinkActive}>
              Habits
            </Link>
          </nav>
          <div className="ml-auto flex items-center gap-2">
            <ThemeToggle />
            <AccountMenu />
          </div>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}

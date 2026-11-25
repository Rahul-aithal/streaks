import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link, Navigate, Outlet, useRouterState } from "@tanstack/react-router"
import { lazy, Suspense } from "react"
import { Logo } from "@/components/logo"
import { SlowHint } from "@/components/loading"
import { ErrorPage } from "@/components/status-page"
import { ThemeToggle } from "@/components/theme-toggle"
import { Skeleton } from "@/components/ui/skeleton"
import { ApiError } from "@/lib/api"
import { meQuery } from "@/lib/use-me"

// The menu (and the dropdown it pulls in) loads after the bar has painted.
const AccountMenu = lazy(() => import("@/components/account-menu").then((m) => ({ default: m.AccountMenu })))

export const Route = createFileRoute("/_app")({
  // Starts the sign-in check with the navigation, in parallel with the page's own data.
  loader: ({ context }) => {
    if (typeof window !== "undefined") void context.queryClient.prefetchQuery(meQuery)
  },
  component: AppLayout,
})

const navLink = "rounded-md px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
const navLinkActive = { className: "bg-muted font-medium !text-foreground" }

// Hovering a tab loads that page's code and starts its request (preload="intent"),
// so the click lands on a page that is already on its way.
// The bar is plain links and a button, so it paints as soon as the app code runs,
// without waiting on the API. Only the avatar and the page wait for the sign-in check.
function AppLayout() {
  const me = useQuery(meQuery)
  const path = useRouterState({ select: (s) => s.location.pathname })

  if (me.isError && me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" search={{}} replace />
  }

  return (
    <div className="flex min-h-svh flex-col">
      <header className="sticky top-0 z-40 border-b bg-background/85 shadow-[0_1px_0_0_var(--border)] backdrop-blur">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-2 px-4">
          <Link to="/" className="mr-2 sm:mr-4" aria-label="Streaks home">
            <Logo />
          </Link>
          <nav className="flex gap-1">
            <Link to="/" preload="intent" className={navLink} activeProps={navLinkActive} activeOptions={{ exact: true }}>
              Today
            </Link>
            <Link to="/habits" preload="intent" className={navLink} activeProps={navLinkActive}>
              Habits
            </Link>
          </nav>
          <div className="ml-auto flex items-center gap-2">
            <ThemeToggle />
            {me.data ? (
              <Suspense fallback={<Skeleton className="size-8 rounded-full" />}>
                <AccountMenu />
              </Suspense>
            ) : (
              <Skeleton className="size-8 rounded-full" />
            )}
          </div>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-5">
        {me.isError ? (
          <ErrorPage error={me.error} onRetry={() => void me.refetch()} fill={false} />
        ) : me.data ? (
          // Keyed by path so each page lands with the entrance animation.
          <div key={path} className="page-enter flex flex-col gap-5">
            <Outlet />
          </div>
        ) : (
          <PageSkeleton />
        )}
      </main>
    </div>
  )
}

function PageSkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-busy>
      <Skeleton className="h-12 w-56" />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-[72px]" />
        ))}
      </div>
      <Skeleton className="h-72" />
      <SlowHint />
    </div>
  )
}

/// <reference types="vite/client" />
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createRootRouteWithContext,
  HeadContent,
  Link,
  Outlet,
  Scripts,
} from "@tanstack/react-router"
import type { ReactNode } from "react"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import styles from "@/styles.css?url"

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Streaks" },
    ],
    links: [
      { rel: "stylesheet", href: styles },
      { rel: "icon", href: "data:," },
    ],
  }),
  shellComponent: Document,
  component: App,
  notFoundComponent: () => <p className="text-muted-foreground">Page not found.</p>,
})

function Document({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

const navLink = "text-sm text-muted-foreground hover:text-foreground"
const navLinkActive = { className: "font-medium !text-foreground" }

function App() {
  const { queryClient } = Route.useRouteContext()
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={100}>
        <div className="mx-auto flex min-h-svh max-w-3xl flex-col gap-6 px-4 py-6">
          <header className="flex items-center gap-6">
            <Link to="/" className="font-semibold">
              Streaks
            </Link>
            <nav className="flex gap-4">
              <Link to="/" className={navLink} activeProps={navLinkActive} activeOptions={{ exact: true }}>
                Today
              </Link>
              <Link to="/habits" className={navLink} activeProps={navLinkActive}>
                Habits
              </Link>
            </nav>
          </header>
          <main className="flex flex-col gap-6">
            <Outlet />
          </main>
        </div>
        <Toaster />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

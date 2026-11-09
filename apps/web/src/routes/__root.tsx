/// <reference types="vite/client" />
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createRootRouteWithContext, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router"
import type { ReactNode } from "react"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { THEME_SCRIPT } from "@/lib/theme"
import styles from "@/styles.css?url"

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Streaks" },
      { name: "theme-color", content: "#007852" },
    ],
    links: [
      { rel: "stylesheet", href: styles },
      { rel: "icon", href: "/favicon.ico", sizes: "48x48" },
      { rel: "icon", href: "/favicon.svg", type: "image/svg+xml" },
      { rel: "apple-touch-icon", href: "/apple-touch-icon.png" },
      { rel: "manifest", href: "/site.webmanifest" },
    ],
  }),
  shellComponent: Document,
  component: App,
  notFoundComponent: () => (
    <div className="flex min-h-svh flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <p>Page not found.</p>
      <Link to="/" className="underline">
        Go to Today
      </Link>
    </div>
  ),
})

function Document({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className="bg-background" suppressHydrationWarning>
      <head>
        {/* biome-ignore lint: a static string defined in this repo. */}
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
        <HeadContent />
      </head>
      <body className="bg-muted/30 antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function App() {
  const { queryClient } = Route.useRouteContext()
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={100}>
        <Outlet />
        <Toaster />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

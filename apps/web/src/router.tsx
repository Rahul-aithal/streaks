import { QueryClient } from "@tanstack/react-query"
import { createRouter } from "@tanstack/react-router"
import { LoadingScreen } from "@/components/loading"
import { ErrorPage, NotFoundPage } from "@/components/status-page"
import { retryDelay, shouldRetry } from "@/lib/query-retry"
import { routeTree } from "./routeTree.gen"

export function getRouter() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 60_000,
        retry: shouldRetry,
        retryDelay,
      },
    },
  })
  return createRouter({
    routeTree,
    context: { queryClient },
    // Hovering a link loads that page's code and starts its data.
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    defaultPendingComponent: LoadingScreen,
    defaultErrorComponent: ({ error, reset }) => <ErrorPage error={error} onRetry={reset} />,
    defaultNotFoundComponent: () => <NotFoundPage />,
    scrollRestoration: true,
  })
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}

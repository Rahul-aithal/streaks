import { QueryClient } from "@tanstack/react-query"
import { createRouter } from "@tanstack/react-router"
import { ApiError } from "@/lib/api"
import { routeTree } from "./routeTree.gen"

export function getRouter() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 60_000,
        // A second try helps with a Neon cold start. Being signed out won't change.
        retry: (count, error) => !(error instanceof ApiError && error.status === 401) && count < 1,
      },
    },
  })
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: false,
    scrollRestoration: true,
  })
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}

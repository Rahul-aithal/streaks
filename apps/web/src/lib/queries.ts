import { api } from "@/lib/api"

// Shared so a route loader can start a request before the component asks for it.
export const summaryQuery = { queryKey: ["summary"], queryFn: api.summary }
export const rewardsQuery = { queryKey: ["rewards"], queryFn: api.rewards }
export const habitsQuery = { queryKey: ["habits"], queryFn: api.habits }
export const historyQuery = (id: number) => ({ queryKey: ["history", id], queryFn: () => api.history(id) })

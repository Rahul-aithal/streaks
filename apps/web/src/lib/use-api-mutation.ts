import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { ApiError } from "@/lib/api"

// Every write can change streaks anywhere, so refetch everything on success.
export function useApiMutation<TVars, TResult>(
  fn: (vars: TVars) => Promise<TResult>,
  onSuccess?: (result: TResult) => void,
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: async (result) => {
      await queryClient.invalidateQueries()
      onSuccess?.(result)
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 401) {
        window.location.assign("/login") // The session ended.
        return
      }
      toast.error(error.message)
    },
  })
}

// Changing who's signed in reloads the page, so nothing cached from one account
// can ever show under another.
export function useSessionChange<TVars, TResult>(
  fn: (vars: TVars) => Promise<TResult>,
  destination: (result: TResult) => string = () => "/",
) {
  return useMutation({
    mutationFn: fn,
    onSuccess: (result) => window.location.assign(destination(result)),
    onError: (error) => toast.error(error.message),
  })
}

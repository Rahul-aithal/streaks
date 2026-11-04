import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

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
    onError: (error) => toast.error(error.message),
  })
}

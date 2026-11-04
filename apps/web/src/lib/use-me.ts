import { useSuspenseQuery } from "@tanstack/react-query"
import { type Account, api } from "@/lib/api"

export const meQuery = { queryKey: ["me"], queryFn: api.me }

// For routes under _app, which has already loaded the user.
export function useMe() {
  const { data } = useSuspenseQuery(meQuery)
  const active = data.accounts.find((a) => a.active) as Account
  return { ...data, active }
}

export function firstName(account: Account) {
  return account.name.split(" ")[0] || account.email.split("@")[0] || ""
}

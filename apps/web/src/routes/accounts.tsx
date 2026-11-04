import { useQuery } from "@tanstack/react-query"
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { Plus } from "lucide-react"
import { useEffect } from "react"
import { toast } from "sonner"
import { ConfirmButton } from "@/components/confirm-button"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type GoogleAccount } from "@/lib/api"
import { formatInstant } from "@/lib/dates"
import { useApiMutation } from "@/lib/use-api-mutation"

interface AccountsSearch {
  connected?: boolean
  google_error?: string
}

export const Route = createFileRoute("/accounts")({
  component: AccountsPage,
  validateSearch: (search: Record<string, unknown>): AccountsSearch => ({
    connected: search.connected ? true : undefined,
    google_error: typeof search.google_error === "string" ? search.google_error : undefined,
  }),
})

const GOOGLE_ERRORS: Record<string, string> = {
  denied: "Google access wasn't approved, so no account was connected.",
  expired: "That login expired. Please try connecting again.",
  missing_scope: "Tasks access wasn't ticked on Google's consent screen. Please try again and allow it.",
  no_refresh_token: "Google didn't grant lasting access. Please try again.",
  failed: "Couldn't connect the account. Check the API logs and try again.",
}

function useLoginResult() {
  const { connected, google_error: error } = Route.useSearch()
  const navigate = useNavigate()

  useEffect(() => {
    if (!connected && !error) return
    // The fixed ID stops a double toast when effects run twice in development.
    if (connected) toast.success("Google account connected.", { id: "google-login" })
    if (error) toast.error(GOOGLE_ERRORS[error] ?? GOOGLE_ERRORS.failed, { id: "google-login" })
    void navigate({ to: "/accounts", search: {}, replace: true })
  }, [connected, error, navigate])
}

function useConnect() {
  return useApiMutation(api.connectGoogle, ({ url }) => window.location.assign(url))
}

function AccountsPage() {
  useLoginResult()
  const accounts = useQuery({ queryKey: ["google-accounts"], queryFn: api.googleAccounts })
  const connect = useConnect()

  return (
    <>
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold">Accounts</h1>
        {accounts.data?.configured && (
          <Button size="sm" disabled={connect.isPending} onClick={() => connect.mutate(undefined)}>
            <Plus />
            Connect Google account
          </Button>
        )}
      </div>
      {accounts.isPending && <Skeleton className="h-32" />}
      {accounts.isError && <QueryError error={accounts.error} onRetry={() => void accounts.refetch()} />}
      {accounts.data && !accounts.data.configured && (
        <Card>
          <CardHeader>
            <CardTitle>Google isn't set up yet</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET and GOOGLE_TOKEN_KEY in .env, then restart. The README has the
            steps.
          </CardContent>
        </Card>
      )}
      {accounts.data?.configured && accounts.data.accounts.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No accounts connected. Tasks from every account you connect appear together on the Today page.
        </p>
      )}
      {accounts.data && accounts.data.accounts.length > 0 && (
        <ul className="flex flex-col gap-2">
          {accounts.data.accounts.map((account) => (
            <AccountItem key={account.id} account={account} configured={accounts.data.configured} />
          ))}
        </ul>
      )}
    </>
  )
}

function AccountItem({ account, configured }: { account: GoogleAccount; configured: boolean }) {
  const connect = useConnect()
  const disconnect = useApiMutation(() => api.disconnectGoogle(account.id))

  return (
    <li className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate font-medium">{account.email}</span>
          {account.needs_reconnect && <Badge variant="destructive">Needs reconnecting</Badge>}
        </div>
        <p className="text-xs text-muted-foreground">
          {account.last_success_at ? `Last synced ${formatInstant(account.last_success_at)}` : "Hasn't synced yet"}
        </p>
        {account.last_error && <p className="break-words text-xs text-destructive">{account.last_error}</p>}
      </div>
      {configured && (
        <div className="flex shrink-0 gap-2">
          {account.needs_reconnect && (
            <Button variant="outline" size="sm" disabled={connect.isPending} onClick={() => connect.mutate(undefined)}>
              Reconnect
            </Button>
          )}
          <ConfirmButton
            title={`Disconnect ${account.email}?`}
            description="This revokes access and removes its tasks from Streaks. Your tasks in Google aren't touched, and past daily counts are kept."
            confirmLabel="Disconnect"
            disabled={disconnect.isPending}
            onConfirm={() => disconnect.mutate(undefined)}
          >
            Disconnect
          </ConfirmButton>
        </div>
      )}
    </li>
  )
}

import { createFileRoute } from "@tanstack/react-router"
import { ExternalLink } from "lucide-react"
import { type FormEvent, useEffect, useState } from "react"
import { toast } from "sonner"
import { AccountAvatar } from "@/components/account-menu"
import { ConfirmButton } from "@/components/confirm-button"
import { GoogleSignInButton } from "@/components/google-button"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, GITHUB_CONNECT_URL, GITHUB_TOKEN_URL, GOOGLE_RECONNECT_URL, type SyncRun } from "@/lib/api"
import { formatInstant } from "@/lib/dates"
import { useApiMutation, useSessionChange } from "@/lib/use-api-mutation"
import { useMe } from "@/lib/use-me"

export const Route = createFileRoute("/_app/settings")({
  component: SettingsPage,
  validateSearch: (search: Record<string, unknown>): { github?: string } => ({
    github: typeof search.github === "string" ? search.github : undefined,
  }),
})

function SyncStatus({ run }: { run: SyncRun }) {
  return (
    <div className="text-xs text-muted-foreground">
      <p>{run.last_success_at ? `Last synced ${formatInstant(run.last_success_at)}` : "Hasn't synced yet"}</p>
      {run.last_error && <p className="break-words text-destructive">Last sync failed: {run.last_error}</p>}
    </div>
  )
}

function SettingsPage() {
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
      <GoogleCard />
      <GitHubCard />
      <DangerCard />
    </div>
  )
}

function GoogleCard() {
  const { active, google } = useMe()
  return (
    <Card>
      <CardHeader>
        <CardTitle>Google account</CardTitle>
        <CardDescription>You sign in with this account, and your tasks come from it.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center gap-3">
        <AccountAvatar account={active} className="size-10" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="truncate font-medium">{active.email}</p>
            {google.needs_reconnect && <Badge variant="destructive">Needs reconnecting</Badge>}
          </div>
          <SyncStatus run={google.sync} />
        </div>
        {google.needs_reconnect && (
          <GoogleSignInButton href={GOOGLE_RECONNECT_URL} size="sm">
            Reconnect
          </GoogleSignInButton>
        )}
      </CardContent>
    </Card>
  )
}

const GITHUB_RESULTS: Record<string, string> = {
  denied: "GitHub connection was cancelled.",
  expired: "That GitHub connection expired. Please try again.",
  failed: "Couldn't connect GitHub. Check the API logs and try again.",
}

function GitHubCard() {
  const { github } = useMe()
  const { github: result } = Route.useSearch()
  const navigate = Route.useNavigate()
  const unlink = useApiMutation(api.unlinkGitHub)

  // Show the outcome of the trip to GitHub once, then tidy the URL.
  useEffect(() => {
    if (!result) return
    // The fixed ID stops a re-run of this effect from showing it twice.
    const options = { id: "github-result" }
    if (result === "connected") toast.success("GitHub connected.", options)
    else toast.error(GITHUB_RESULTS[result] ?? GITHUB_RESULTS.failed, options)
    void navigate({ search: {}, replace: true })
  }, [result, navigate])

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub</CardTitle>
        <CardDescription>
          A GitHub habit is completed automatically on days you push or open a pull request.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {github.login ? (
          <div className="flex flex-wrap items-center gap-3">
            <span className="flex size-10 items-center justify-center rounded-full bg-muted">
              <GitHubIcon className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">{github.login}</p>
              <SyncStatus run={github.sync} />
            </div>
            <ConfirmButton
              title="Disconnect GitHub?"
              description="Syncing stops and access is removed. Your past activity and check-ins are kept."
              confirmLabel="Disconnect"
              disabled={unlink.isPending}
              onConfirm={() => unlink.mutate(undefined)}
            >
              Disconnect
            </ConfirmButton>
          </div>
        ) : github.connect_enabled ? (
          <div className="flex flex-col items-start gap-2">
            <Button asChild>
              {/* A real navigation, because this leaves the app for GitHub. */}
              <a href={GITHUB_CONNECT_URL}>
                <GitHubIcon className="size-4" />
                Connect GitHub
              </a>
            </Button>
            <p className="text-xs text-muted-foreground">Counts activity in your public repositories.</p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            To get a Connect button, add a GitHub OAuth app to <code>.env</code> (see the README). Or use a token below.
          </p>
        )}
        <GitHubTokenForm linked={github.login !== null} defaultOpen={!github.login && !github.connect_enabled} />
      </CardContent>
    </Card>
  )
}

function GitHubTokenForm({ linked, defaultOpen }: { linked: boolean; defaultOpen: boolean }) {
  const [token, setToken] = useState("")
  const link = useApiMutation(
    (value: string) => api.linkGitHub(value),
    ({ login }) => {
      setToken("")
      toast.success(`Connected GitHub account ${login}.`)
    },
  )
  const submit = (e: FormEvent) => {
    e.preventDefault()
    link.mutate(token)
  }

  return (
    <details open={defaultOpen} className="text-sm">
      <summary className="cursor-pointer text-muted-foreground">
        {linked ? "Replace with a token" : "Include private repositories with a token"}
      </summary>
      <form onSubmit={submit} className="mt-3 flex flex-col gap-2">
        <Label htmlFor="github-token">Personal access token</Label>
        <div className="flex gap-2">
          <Input
            id="github-token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            placeholder="github_pat_…"
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
          <Button type="submit" disabled={link.isPending || token.trim() === ""}>
            {link.isPending ? "Connecting…" : "Connect"}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          <a href={GITHUB_TOKEN_URL} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 underline">
            Create a fine-grained token
            <ExternalLink className="size-3" />
          </a>{" "}
          with the Events user permission set to read-only. It's stored encrypted.
        </p>
      </form>
    </details>
  )
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
      <path d="M12 .3a12 12 0 0 0-3.800 23.400c.6.100.8-.300.8-.600v-2.100c-3.300.700-4-1.600-4-1.600-.5-1.400-1.300-1.800-1.300-1.800-1.100-.700.1-.700.1-.700 1.200.100 1.800 1.200 1.800 1.200 1.100 1.800 2.800 1.300 3.500 1 .1-.800.4-1.300.8-1.600-2.700-.300-5.500-1.300-5.500-5.900 0-1.300.5-2.400 1.200-3.200-.100-.300-.500-1.500.1-3.200 0 0 1-.300 3.300 1.200a11.500 11.500 0 0 1 6 0c2.300-1.500 3.300-1.200 3.300-1.200.6 1.700.2 2.900.1 3.200.8.800 1.200 1.900 1.200 3.200 0 4.600-2.800 5.600-5.500 5.900.4.400.8 1.100.8 2.200v3.300c0 .3.200.7.800.6A12 12 0 0 0 12 .3z" />
    </svg>
  )
}

function DangerCard() {
  const { active } = useMe()
  const remove = useSessionChange(api.deleteAccount, ({ signed_in }) => (signed_in ? "/" : "/login"))
  return (
    <Card>
      <CardHeader>
        <CardTitle>Delete account</CardTitle>
        <CardDescription>
          Permanently deletes this account's habits, check-ins, streaks and tasks from Streaks, and revokes Google and GitHub access.
          Your data in Google and GitHub isn't touched.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ConfirmButton
          title={`Delete ${active.email} from Streaks?`}
          description="All of this account's habits and history will be gone for good. This can't be undone."
          confirmLabel="Delete account"
          variant="destructive"
          disabled={remove.isPending}
          onConfirm={() => remove.mutate(undefined)}
        >
          Delete account
        </ConfirmButton>
      </CardContent>
    </Card>
  )
}

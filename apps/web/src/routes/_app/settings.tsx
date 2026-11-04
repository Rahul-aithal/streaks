import { createFileRoute } from "@tanstack/react-router"
import { ExternalLink, GitBranch } from "lucide-react"
import { type FormEvent, useState } from "react"
import { toast } from "sonner"
import { AccountAvatar } from "@/components/account-menu"
import { ConfirmButton } from "@/components/confirm-button"
import { GoogleSignInButton } from "@/components/google-button"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, GITHUB_TOKEN_URL, GOOGLE_RECONNECT_URL, type SyncRun } from "@/lib/api"
import { formatInstant } from "@/lib/dates"
import { useApiMutation, useSessionChange } from "@/lib/use-api-mutation"
import { useMe } from "@/lib/use-me"

export const Route = createFileRoute("/_app/settings")({ component: SettingsPage })

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

function GitHubCard() {
  const { github } = useMe()
  const [token, setToken] = useState("")
  const link = useApiMutation(
    (value: string) => api.linkGitHub(value),
    ({ login }) => {
      setToken("")
      toast.success(`Linked GitHub account ${login}.`)
    },
  )
  const unlink = useApiMutation(api.unlinkGitHub)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    link.mutate(token)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub</CardTitle>
        <CardDescription>
          A GitHub habit is completed automatically on days you push or open a pull request.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {github.login && (
          <div className="flex flex-wrap items-center gap-3">
            <span className="flex size-10 items-center justify-center rounded-full bg-muted">
              <GitBranch className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">{github.login}</p>
              <SyncStatus run={github.sync} />
            </div>
            <ConfirmButton
              title="Unlink GitHub?"
              description="Syncing stops and the token is removed. Your past activity and check-ins are kept."
              confirmLabel="Unlink"
              disabled={unlink.isPending}
              onConfirm={() => unlink.mutate(undefined)}
            >
              Unlink
            </ConfirmButton>
          </div>
        )}
        <form onSubmit={submit} className="flex flex-col gap-2">
          <Label htmlFor="github-token">{github.login ? "Replace token" : "Personal access token"}</Label>
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
              {link.isPending ? "Linking…" : "Link"}
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
      </CardContent>
    </Card>
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
          Permanently deletes this account's habits, check-ins, streaks and tasks from Streaks, and revokes Google access.
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

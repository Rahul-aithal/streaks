import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link, Navigate } from "@tanstack/react-router"
import { CheckCircle2, Flame, GitBranch } from "lucide-react"
import { GoogleSignInButton } from "@/components/google-button"
import { Logo } from "@/components/logo"
import { Card, CardContent } from "@/components/ui/card"
import { meQuery } from "@/lib/use-me"

const ERRORS: Record<string, string> = {
  denied: "Google sign-in was cancelled.",
  expired: "That sign-in expired. Please try again.",
  missing_scope: "Streaks needs access to Google Tasks. Please try again and tick the Tasks box.",
  not_allowed: "This Google account isn't allowed. Add its email to ALLOWED_EMAILS in .env, then restart.",
  unverified: "Google hasn't verified this account's email address.",
  failed: "Sign-in failed. Check the API logs and try again.",
}

export const Route = createFileRoute("/login")({
  component: LoginPage,
  validateSearch: (search: Record<string, unknown>): { error?: string } => ({
    error: typeof search.error === "string" ? search.error : undefined,
  }),
})

const FEATURES = [
  { icon: Flame, text: "Streaks worked out from your check-ins, so they can't drift" },
  { icon: GitBranch, text: "GitHub activity completes your coding habit automatically" },
  { icon: CheckCircle2, text: "Today's habits and Google Tasks in one place" },
]

function LoginPage() {
  const { error } = Route.useSearch()
  const me = useQuery(meQuery)

  // Already signed in: only stay here to show why adding an account failed.
  if (me.data && !error) return <Navigate to="/" replace />

  return (
    <main className="flex min-h-svh items-center justify-center p-4">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <Logo className="justify-center text-lg" />
        <Card>
          <CardContent className="flex flex-col gap-5">
            <div className="text-center">
              <h1 className="text-xl font-semibold">Sign in to Streaks</h1>
              <p className="mt-1 text-sm text-muted-foreground">Keep your habits going, one day at a time.</p>
            </div>
            {error && (
              <p role="alert" className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
                {ERRORS[error] ?? ERRORS.failed}
              </p>
            )}
            <GoogleSignInButton size="lg" className="w-full" />
            {me.data && (
              <Link to="/" className="text-center text-sm text-muted-foreground underline">
                Back to Streaks
              </Link>
            )}
          </CardContent>
        </Card>
        <ul className="flex flex-col gap-2 px-1 text-sm text-muted-foreground">
          {FEATURES.map(({ icon: Icon, text }) => (
            <li key={text} className="flex items-start gap-2">
              <Icon className="mt-0.5 size-4 shrink-0 text-primary" />
              {text}
            </li>
          ))}
        </ul>
      </div>
    </main>
  )
}

import { Check, LogOut, Plus, Settings } from "lucide-react"
import { Link } from "@tanstack/react-router"
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { type Account, api, GOOGLE_SIGN_IN_URL } from "@/lib/api"
import { useMe } from "@/lib/use-me"
import { useSessionChange } from "@/lib/use-api-mutation"
import { cn } from "@/lib/utils"

export function AccountAvatar({ account, className }: { account: Account; className?: string }) {
  return (
    <Avatar className={cn("size-8", className)}>
      {/* Google's image host refuses requests that send a referrer. */}
      <AvatarImage src={account.avatar_url} alt="" referrerPolicy="no-referrer" />
      <AvatarFallback>{(account.name || account.email).charAt(0).toUpperCase()}</AvatarFallback>
    </Avatar>
  )
}

const afterSignOut = ({ signed_in }: { signed_in: boolean }) => (signed_in ? "/" : "/login")

export function AccountMenu() {
  const { accounts, active } = useMe()
  const others = accounts.filter((a) => !a.active)
  const switchTo = useSessionChange(api.switchAccount)
  const signOut = useSessionChange(api.signOut, afterSignOut)
  const signOutAll = useSessionChange(api.signOutAll, () => "/login")

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={`Account menu for ${active.email}`}
        className="rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <AccountAvatar account={active} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel className="font-normal">
          <div className="flex items-center gap-3">
            <AccountAvatar account={active} className="size-10" />
            <div className="min-w-0">
              <p className="truncate font-medium text-foreground">{active.name || active.email}</p>
              <p className="truncate text-xs text-muted-foreground">{active.email}</p>
            </div>
            <Check className="ml-auto size-4 shrink-0 text-primary" aria-label="Active account" />
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {others.length > 0 && (
          <>
            <DropdownMenuLabel className="text-xs text-muted-foreground">Switch account</DropdownMenuLabel>
            {others.map((account) => (
              <DropdownMenuItem
                key={account.id}
                disabled={switchTo.isPending}
                onSelect={() => switchTo.mutate(account.id)}
              >
                <AccountAvatar account={account} className="size-6" />
                <span className="truncate">{account.email}</span>
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuItem asChild>
          <a href={GOOGLE_SIGN_IN_URL}>
            <Plus />
            Add account
          </a>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/settings">
            <Settings />
            Settings
          </Link>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={signOut.isPending} onSelect={() => signOut.mutate(undefined)}>
          <LogOut />
          <span className="truncate">Sign out of {active.email}</span>
        </DropdownMenuItem>
        {others.length > 0 && (
          <DropdownMenuItem disabled={signOutAll.isPending} onSelect={() => signOutAll.mutate(undefined)}>
            <LogOut />
            Sign out of all accounts
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

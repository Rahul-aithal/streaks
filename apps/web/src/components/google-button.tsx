import type { ComponentProps } from "react"
import { Button } from "@/components/ui/button"
import { GOOGLE_SIGN_IN_URL } from "@/lib/api"

export function GoogleIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="size-4">
      <path fill="#4285F4" d="M23.5 12.3c0-.8-.1-1.600-.2-2.300H12v4.400h6.500a5.500 5.500 0 0 1-2.400 3.600v3h3.900c2.300-2.100 3.500-5.200 3.500-8.700z" />
      <path fill="#34A853" d="M12 24c3.200 0 6-1.100 8-2.900l-3.900-3c-1.100.7-2.500 1.200-4.100 1.200-3.100 0-5.800-2.100-6.700-5H1.300v3.100A12 12 0 0 0 12 24z" />
      <path fill="#FBBC05" d="M5.300 14.300a7.200 7.200 0 0 1 0-4.600V6.600h-4a12 12 0 0 0 0 10.800l4-3.100z" />
      <path fill="#EA4335" d="M12 4.800c1.800 0 3.300.6 4.600 1.800l3.400-3.400A12 12 0 0 0 1.300 6.600l4 3.100c.9-2.900 3.600-4.900 6.700-4.900z" />
    </svg>
  )
}

export function GoogleSignInButton({
  href = GOOGLE_SIGN_IN_URL,
  children = "Continue with Google",
  ...props
}: ComponentProps<typeof Button> & { href?: string }) {
  return (
    <Button variant="outline" asChild {...props}>
      {/* A real navigation, because this leaves the app for Google. */}
      <a href={href}>
        <GoogleIcon />
        {children}
      </a>
    </Button>
  )
}

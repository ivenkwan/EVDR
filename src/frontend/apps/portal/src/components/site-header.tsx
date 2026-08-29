"use client"

import Link from "next/link"

import { Button } from "@evdr/ui"
import { Menu, ShieldCheck, X } from "lucide-react"

import { usePortalStore } from "@/stores/use-portal-store"

const NAV_ITEMS = [
  { href: "/", label: "Home" },
  { href: "/rooms", label: "Data rooms" },
]

/**
 * Guest-facing header (client component: mobile menu state lives in the
 * portal Zustand store — TR-3.3). Brand mark + public navigation only.
 */
export function SiteHeader() {
  const mobileMenuOpen = usePortalStore((s) => s.mobileMenuOpen)
  const setMobileMenuOpen = usePortalStore((s) => s.setMobileMenuOpen)

  return (
    <header className="border-b bg-background">
      <div className="mx-auto flex h-16 w-full max-w-6xl items-center justify-between px-4 sm:px-6">
        <Link
          href="/"
          className="flex items-center gap-2 font-semibold tracking-tight"
        >
          <ShieldCheck className="size-5 text-primary" aria-hidden />
          <span>EVDR Portal</span>
        </Link>

        <nav className="hidden items-center gap-6 sm:flex" aria-label="Main">
          {NAV_ITEMS.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className="text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
            >
              {item.label}
            </Link>
          ))}
        </nav>

        <Button asChild variant="outline" size="sm" className="hidden sm:inline-flex">
          <Link href="/rooms">Enter a room</Link>
        </Button>

        <button
          type="button"
          className="inline-flex size-9 items-center justify-center rounded-md border sm:hidden"
          aria-label={mobileMenuOpen ? "Close menu" : "Open menu"}
          aria-expanded={mobileMenuOpen}
          onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
        >
          {mobileMenuOpen ? (
            <X className="size-4" aria-hidden />
          ) : (
            <Menu className="size-4" aria-hidden />
          )}
        </button>
      </div>

      {mobileMenuOpen ? (
        <nav className="border-t px-4 py-3 sm:hidden" aria-label="Mobile">
          {NAV_ITEMS.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              onClick={() => setMobileMenuOpen(false)}
              className="block rounded-md px-2 py-2 text-sm font-medium text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            >
              {item.label}
            </Link>
          ))}
          <Button asChild size="sm" className="mt-2 w-full">
            <Link href="/rooms" onClick={() => setMobileMenuOpen(false)}>
              Enter a room
            </Link>
          </Button>
        </nav>
      ) : null}
    </header>
  )
}

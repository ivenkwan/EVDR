"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"

import { cn } from "@evdr/ui"
import {
  FolderClosed,
  LayoutDashboard,
  Settings,
  ShieldCheck,
  Users,
  type LucideIcon,
} from "lucide-react"

import { useAdminStore } from "@/stores/use-admin-store"

interface NavItem {
  href: string
  label: string
  icon: LucideIcon
}

const NAV_ITEMS: NavItem[] = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/rooms", label: "Rooms", icon: FolderClosed },
  { href: "/users", label: "Users", icon: Users },
  { href: "/settings", label: "Settings", icon: Settings },
]

/**
 * Internal console navigation (client component: active-route highlight via
 * usePathname, mobile drawer state via the admin Zustand store — TR-3.3).
 * Never reachable from the external portal (FR-4.7 / TR-3.4).
 */
export function AdminSidebar() {
  const pathname = usePathname()
  const open = useAdminStore((s) => s.sidebarOpen)
  const setOpen = useAdminStore((s) => s.setSidebarOpen)

  return (
    <>
      {open ? (
        <div
          className="fixed inset-0 z-40 bg-background/60 lg:hidden"
          onClick={() => setOpen(false)}
          aria-hidden
        />
      ) : null}

      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-50 w-64 shrink-0 border-r bg-background transition-transform lg:static lg:translate-x-0",
          open ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div className="flex h-16 items-center gap-2 border-b px-6">
          <ShieldCheck className="size-5 text-primary" aria-hidden />
          <span className="font-semibold tracking-tight">EVDR Admin</span>
        </div>

        <nav className="space-y-1 p-3" aria-label="Admin">
          {NAV_ITEMS.map(({ href, label, icon: Icon }) => {
            const active = pathname.startsWith(href)
            return (
              <Link
                key={href}
                href={href}
                onClick={() => setOpen(false)}
                className={cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  active
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                )}
                aria-current={active ? "page" : undefined}
              >
                <Icon className="size-4" aria-hidden />
                {label}
              </Link>
            )
          })}
        </nav>

        <div className="absolute inset-x-0 bottom-0 border-t p-4 text-xs text-muted-foreground">
          Internal use only — tenant admin &amp; platform operator.
        </div>
      </aside>
    </>
  )
}

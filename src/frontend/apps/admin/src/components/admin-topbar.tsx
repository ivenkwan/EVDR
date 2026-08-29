"use client"

import { Menu } from "lucide-react"

import { useAdminStore } from "@/stores/use-admin-store"

/** Console topbar: mobile sidebar trigger + context line. */
export function AdminTopbar() {
  const setOpen = useAdminStore((s) => s.setSidebarOpen)

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center gap-4 border-b bg-background px-4 lg:px-8">
      <button
        type="button"
        className="inline-flex size-9 items-center justify-center rounded-md border lg:hidden"
        aria-label="Open navigation"
        onClick={() => setOpen(true)}
      >
        <Menu className="size-4" aria-hidden />
      </button>
      <span className="text-sm text-muted-foreground">
        Internal console — tenant administration &amp; platform operations
      </span>
    </header>
  )
}

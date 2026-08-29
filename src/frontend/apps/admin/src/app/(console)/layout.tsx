import type { ReactNode } from "react"

import { AdminSidebar } from "@/components/admin-sidebar"
import { AdminTopbar } from "@/components/admin-topbar"

/**
 * Internal console shell (TR-3.4 / FR-4.7): sidebar + topbar, reachable only
 * by tenant admins and platform operators — never by external entities.
 */
export default function ConsoleLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh bg-muted/40">
      <AdminSidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <AdminTopbar />
        <main className="flex-1 p-6 lg:p-8">{children}</main>
      </div>
    </div>
  )
}

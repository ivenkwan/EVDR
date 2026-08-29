import type { Metadata } from "next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { FolderLock } from "lucide-react"

export const metadata: Metadata = {
  title: "Data rooms",
}

export default function RoomsPage() {
  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-12 sm:px-6">
      <div className="flex items-center gap-3">
        <FolderLock className="size-6 text-primary" aria-hidden />
        <h1 className="text-2xl font-semibold tracking-tight">Data rooms</h1>
      </div>
      <p className="mt-2 max-w-2xl text-muted-foreground">
        Rooms you can access appear here. Each room is invitation-only and every
        page view is audit-logged.
      </p>

      <Card className="mt-8">
        <CardHeader>
          <CardTitle>No rooms yet</CardTitle>
          <CardDescription>
            Your host will send a secure invitation link when a room is shared with
            you.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Room listing is a Phase 1 follow-up wave — this is the routing shell
          (TR-3.1). Access gates, NDA/consent and the secure viewer arrive later
          (FR-1.x).
        </CardContent>
      </Card>
    </div>
  )
}

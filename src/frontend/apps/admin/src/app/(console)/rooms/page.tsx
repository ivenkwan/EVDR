import type { Metadata } from "next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"

export const metadata: Metadata = {
  title: "Rooms",
}

export default function AdminRoomsPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Rooms</h1>
      <Card>
        <CardHeader>
          <CardTitle>Room management</CardTitle>
          <CardDescription>
            Create, configure and monitor data rooms and their permission tiers.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Coming in a later wave — room CRUD, permission tiers (FR-1.2) and
          watermark policy presets (FR-1.3).
        </CardContent>
      </Card>
    </div>
  )
}

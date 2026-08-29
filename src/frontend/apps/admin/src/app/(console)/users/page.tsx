import type { Metadata } from "next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"

export const metadata: Metadata = {
  title: "Users",
}

export default function AdminUsersPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Users</h1>
      <Card>
        <CardHeader>
          <CardTitle>User administration</CardTitle>
          <CardDescription>
            Internal staff, guest invitations and role management.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Coming in a later wave — invitations, expiring guest access (FR-4.3)
          and role-based access control.
        </CardContent>
      </Card>
    </div>
  )
}

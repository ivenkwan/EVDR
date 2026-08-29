import type { Metadata } from "next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"

export const metadata: Metadata = {
  title: "Dashboard",
}

const STATS = [
  { label: "Active rooms", value: "—" },
  { label: "Users", value: "—" },
  { label: "Documents", value: "—" },
  { label: "Audit events (24h)", value: "—" },
]

export default function DashboardPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
        <p className="mt-1 text-muted-foreground">
          Console overview — metrics populate once backend APIs are wired
          (TR-3.3).
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {STATS.map((stat) => (
          <Card key={stat.label}>
            <CardHeader>
              <CardDescription>{stat.label}</CardDescription>
              <CardTitle className="text-3xl">{stat.value}</CardTitle>
            </CardHeader>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Getting started</CardTitle>
          <CardDescription>
            Scaffold baseline (TR-3.1–3.4): routing shells and state wiring only.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Room management, user administration and audit views arrive in later
          waves. All server state flows through TanStack Query against the API
          gateway.
        </CardContent>
      </Card>
    </div>
  )
}

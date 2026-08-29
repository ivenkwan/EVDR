import type { Metadata } from "next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"

export const metadata: Metadata = {
  title: "Settings",
}

export default function AdminSettingsPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
      <Card>
        <CardHeader>
          <CardTitle>Console settings</CardTitle>
          <CardDescription>
            Branding, security policy and platform defaults.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Coming in a later wave — console-level configuration surfaces.
        </CardContent>
      </Card>
    </div>
  )
}

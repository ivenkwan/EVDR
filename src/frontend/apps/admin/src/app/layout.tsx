import type { Metadata } from "next"
import type { ReactNode } from "react"

import { AppProviders } from "@/providers/app-providers"

import "./globals.css"

export const metadata: Metadata = {
  title: {
    default: "EVDR Admin Console",
    template: "%s · EVDR Admin",
  },
  description: "EVDR admin console — tenant administration and platform operations.",
}

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-dvh bg-background font-sans text-foreground antialiased">
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  )
}

"use client"

import { useState, type ReactNode } from "react"

import { QueryClientProvider } from "@tanstack/react-query"

import { createQueryClient } from "@/lib/query-client"

/**
 * App-wide providers (TR-3.3). The QueryClient is created once per mount —
 * never as a module-level singleton — so React 19 Strict Mode and HMR keep a
 * single client per provider instance.
 */
export function AppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => createQueryClient())

  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

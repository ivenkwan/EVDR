import { QueryClient } from "@tanstack/react-query"

/**
 * TanStack Query client factory — the one place server-state defaults are
 * tuned for a VDR (TR-3.3): fresh-enough data, conservative retry, no
 * window-focus refetch churn on guest pages.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 60_000,
        retry: 1,
        refetchOnWindowFocus: false,
      },
    },
  })
}

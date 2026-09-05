import type { Metadata } from "next"

import { RoomsManager } from "@/components/rooms/rooms-manager"

export const metadata: Metadata = {
  title: "Rooms",
}

/**
 * Room management (admin console). Data flows through TanStack Query against
 * the same-origin BFF (/api/v1/rooms), which proxies the Room Service and
 * Zod-validates every response.
 */
export default function AdminRoomsPage() {
  return <RoomsManager />
}

import type { Metadata } from "next"

import { RoomDetail } from "@/components/rooms/room-detail"

export const metadata: Metadata = {
  title: "Room",
}

interface RoomPageProps {
  params: Promise<{ roomId: string }>
}

/**
 * Room detail: documents + immutable versions (FR-2.2), upload (FR-2.1),
 * grants, seal and export. The heavy lifting lives in client components —
 * server state flows through TanStack Query against the BFF.
 */
export default async function AdminRoomPage({ params }: RoomPageProps) {
  const { roomId } = await params
  return <RoomDetail roomId={roomId} />
}

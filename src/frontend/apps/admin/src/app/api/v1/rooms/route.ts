/**
 * BFF: list + create rooms (TR-10.1). Proxies the Room Service and validates
 * every payload with the wire schemas — the client never sees an unvalidated
 * upstream response.
 */
import { NextResponse } from "next/server"

import { bffError, roomServicePath } from "@/lib/bff"
import {
  createRoomRequestSchema,
  roomListSchema,
  roomSchema,
} from "@/lib/room-schemas"
import { roomServiceJson } from "@/lib/room-service"

export const dynamic = "force-dynamic"

export async function GET() {
  try {
    const data = await roomServiceJson(
      roomServicePath("/rooms"),
      { method: "GET" },
      roomListSchema,
    )
    return NextResponse.json(data)
  } catch (error) {
    return bffError(error)
  }
}

export async function POST(request: Request) {
  try {
    const raw: unknown = await request.json()
    const parsed = createRoomRequestSchema.safeParse(raw)
    if (!parsed.success) {
      return bffError(parsed.error)
    }
    const room = await roomServiceJson(
      roomServicePath("/rooms"),
      { method: "POST", body: JSON.stringify(parsed.data) },
      roomSchema,
    )
    return NextResponse.json(room, { status: 201 })
  } catch (error) {
    return bffError(error)
  }
}

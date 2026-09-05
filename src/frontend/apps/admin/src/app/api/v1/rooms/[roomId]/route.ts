/**
 * BFF: single room (GET) — proxies the Room Service.
 */
import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath } from "@/lib/bff"
import { roomSchema } from "@/lib/room-schemas"
import { roomServiceJson } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string }>
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const parsed = idParamSchema.safeParse(roomId)
    if (!parsed.success) {
      return NextResponse.json(
        { code: "bad_request", message: "invalid roomId" },
        { status: 400 },
      )
    }
    const room = await roomServiceJson(
      roomServicePath("/rooms", encodeURIComponent(parsed.data)),
      { method: "GET" },
      roomSchema,
    )
    return NextResponse.json(room)
  } catch (error) {
    return bffError(error)
  }
}

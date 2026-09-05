/**
 * BFF: revoke a room access grant (FR-4.4) through the Room Service.
 * Idempotent-safe: revoking an already-revoked grant is a 204.
 */
import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath } from "@/lib/bff"
import { roomServiceErrorFrom, roomServiceFetch } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string; grantId: string }>
}

export async function DELETE(_request: Request, context: RouteContext) {
  try {
    const { roomId, grantId } = await context.params
    const room = idParamSchema.safeParse(roomId)
    const grant = idParamSchema.safeParse(grantId)
    if (!room.success || !grant.success) {
      return NextResponse.json({ code: "bad_request", message: "invalid id" }, { status: 400 })
    }
    const response = await roomServiceFetch(
      roomServicePath(
        "/rooms",
        encodeURIComponent(room.data),
        "/grants",
        encodeURIComponent(grant.data),
      ),
      { method: "DELETE" },
    )
    if (!response.ok) {
      throw await roomServiceErrorFrom(response)
    }
    return new Response(null, { status: 204 })
  } catch (error) {
    return bffError(error)
  }
}

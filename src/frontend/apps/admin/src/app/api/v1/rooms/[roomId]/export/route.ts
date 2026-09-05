/**
 * BFF: export a room as a tar archive (FR-1.7/SR-5.2). GET triggers the
 * export (audited upstream) and streams the archive through with the Room
 * Service's own content headers. All versions are included; audit-trail
 * inclusion is a later policy decision.
 */
import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath } from "@/lib/bff"
import { roomServiceErrorFrom, roomServiceFetch } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string }>
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const room = idParamSchema.safeParse(roomId)
    if (!room.success) {
      return NextResponse.json({ code: "bad_request", message: "invalid roomId" }, { status: 400 })
    }
    const response = await roomServiceFetch(
      roomServicePath("/rooms", encodeURIComponent(room.data), "/export"),
      {
        method: "POST",
        body: JSON.stringify({ includeAuditTrail: false, includeVersions: true }),
      },
    )
    if (!response.ok) {
      throw await roomServiceErrorFrom(response)
    }
    const headers = new Headers()
    const contentType = response.headers.get("content-type")
    if (contentType) {
      headers.set("content-type", contentType)
    }
    const disposition = response.headers.get("content-disposition")
    if (disposition) {
      headers.set("content-disposition", disposition)
    }
    return new Response(response.body, { status: 200, headers })
  } catch (error) {
    return bffError(error)
  }
}

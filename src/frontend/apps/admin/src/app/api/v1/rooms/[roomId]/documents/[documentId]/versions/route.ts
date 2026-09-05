/**
 * BFF: immutable version history of one document (FR-2.2) — live SPI read
 * through the Room Service (ListVersions), ascending by version number.
 */
import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath, validateUpstreamJson } from "@/lib/bff"
import { versionListSchema } from "@/lib/room-schemas"
import { roomServiceFetch } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string; documentId: string }>
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { roomId, documentId } = await context.params
    const room = idParamSchema.safeParse(roomId)
    const doc = idParamSchema.safeParse(documentId)
    if (!room.success || !doc.success) {
      return NextResponse.json({ code: "bad_request", message: "invalid id" }, { status: 400 })
    }
    const response = await roomServiceFetch(
      roomServicePath(
        "/rooms",
        encodeURIComponent(room.data),
        "/documents",
        encodeURIComponent(doc.data),
        "/versions",
      ),
      { method: "GET" },
    )
    return NextResponse.json(await validateUpstreamJson(response, versionListSchema))
  } catch (error) {
    return bffError(error)
  }
}

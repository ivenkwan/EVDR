/**
 * BFF: seal a room (legal hold, FR-1.6) through the Room Service.
 */
import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath, validateUpstreamJson } from "@/lib/bff"
import { sealReceiptSchema } from "@/lib/room-schemas"
import { roomServiceFetch } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string }>
}

export async function POST(request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const room = idParamSchema.safeParse(roomId)
    if (!room.success) {
      return NextResponse.json({ code: "bad_request", message: "invalid roomId" }, { status: 400 })
    }
    const raw: unknown = await request.json().catch(() => null)
    const body: { reason?: string; legalHoldRef?: string } =
      raw && typeof raw === "object" ? (raw as { reason?: string; legalHoldRef?: string }) : {}
    const response = await roomServiceFetch(
      roomServicePath("/rooms", encodeURIComponent(room.data), "/seal"),
      { method: "POST", body: JSON.stringify({ reason: body.reason ?? "", legalHoldRef: body.legalHoldRef ?? "" }) },
    )
    return NextResponse.json(await validateUpstreamJson(response, sealReceiptSchema))
  } catch (error) {
    return bffError(error)
  }
}

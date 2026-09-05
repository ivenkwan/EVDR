/**
 * BFF: room access grants (FR-4.4 lifecycle through the Room Service).
 *
 * GET  — active grants (projection of SPI-confirmed grants).
 * POST — grant access at a permission tier; guest grants must carry an
 *        expiry (the Room Service enforces the SPI's ErrInvalidGrant).
 */
import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath, validateUpstreamJson } from "@/lib/bff"
import { grantListSchema, grantRequestSchema, grantSchema } from "@/lib/room-schemas"
import { roomServiceFetch } from "@/lib/room-service"

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
      roomServicePath("/rooms", encodeURIComponent(room.data), "/grants"),
      { method: "GET" },
    )
    return NextResponse.json(await validateUpstreamJson(response, grantListSchema))
  } catch (error) {
    return bffError(error)
  }
}

export async function POST(request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const room = idParamSchema.safeParse(roomId)
    if (!room.success) {
      return NextResponse.json({ code: "bad_request", message: "invalid roomId" }, { status: 400 })
    }
    const raw: unknown = await request.json()
    const body = grantRequestSchema.safeParse(raw)
    if (!body.success) {
      return bffError(body.error)
    }
    const response = await roomServiceFetch(
      roomServicePath("/rooms", encodeURIComponent(room.data), "/grants"),
      { method: "POST", body: JSON.stringify(body.data) },
    )
    return NextResponse.json(await validateUpstreamJson(response, grantSchema), { status: 201 })
  } catch (error) {
    return bffError(error)
  }
}

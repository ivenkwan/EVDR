/**
 * BFF: public room branding (FR-1.1) for the external portal.
 *
 * This route is the ONLY Room Service surface the portal reaches. It serves
 * the public branding projection — name, slug, state, branding — and nothing
 * else: the Room Service's public endpoint deliberately carries no
 * documents, grants, retention or audit data, and this route validates the
 * response against the public wire schema before returning it.
 */
import { NextResponse } from "next/server"

import { publicRoomSchema } from "@/lib/room-schemas"
import { publicRoomErrorFrom, ROOM_SERVICE_URL } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string }>
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const token = process.env.ROOM_SERVICE_TOKEN
    if (!token) {
      return NextResponse.json(
        { code: "bff_error", message: "Room Service credential is not configured." },
        { status: 500 },
      )
    }
    const response = await fetch(
      `${ROOM_SERVICE_URL}/api/v1/public/rooms/${encodeURIComponent(roomId)}/branding`,
      {
        method: "GET",
        headers: { authorization: `Bearer ${token}` },
        cache: "no-store",
      },
    )
    if (response.status === 404) {
      return NextResponse.json(
        { code: "room_not_found", message: "This room does not exist." },
        { status: 404 },
      )
    }
    if (!response.ok) {
      const err = await publicRoomErrorFrom(response)
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status })
    }
    const raw: unknown = await response.json()
    const parsed = publicRoomSchema.safeParse(raw)
    if (!parsed.success) {
      return NextResponse.json(
        { code: "bff_error", message: "Room Service branding drifted from the contract." },
        { status: 502 },
      )
    }
    return NextResponse.json(parsed.data)
  } catch {
    return NextResponse.json(
      { code: "bff_error", message: "The room service could not be reached." },
      { status: 502 },
    )
  }
}

/**
 * Shared helpers for the admin console BFF route handlers
 * (src/app/api/v1/**). Server-side only.
 */
import { NextResponse } from "next/server"
import { z } from "zod"

import { RoomServiceError, roomServiceErrorFrom } from "@/lib/room-service"

export const idParamSchema = z.string().min(1).max(300)

/** Path-parameter schema shared by every /api/v1/rooms/... handler. */
export function roomServicePath(...segments: string[]): string {
  // Each segment may or may not carry a leading slash; join without ever
  // producing "//" (Go's ServeMux 307-redirects on double slashes and
  // node:http does not normalise them the way fetch does).
  const joined = segments.map((segment) => (segment.startsWith("/") ? segment : `/${segment}`)).join("")
  return `/api/v1${joined}`
}

/** Validates a JSON upstream body against the wire schema. */
export async function validateUpstreamJson<T>(
  response: Response,
  schema: z.ZodType<T>,
): Promise<T> {
  if (!response.ok) {
    throw await roomServiceErrorFrom(response)
  }
  const raw: unknown = await response.json()
  const parsed = schema.safeParse(raw)
  if (!parsed.success) {
    throw new Error(
      `Room Service response drifted from the contract: ${parsed.error.issues
        .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
        .join("; ")}`,
    )
  }
  return parsed.data
}

/** Renders any BFF error as the client's {code,message} envelope. */
export function bffError(error: unknown): NextResponse {
  if (error instanceof RoomServiceError) {
    return NextResponse.json(
      { code: error.code, message: error.message },
      { status: error.status },
    )
  }
  if (error instanceof z.ZodError) {
    return NextResponse.json(
      {
        code: "bad_request",
        message: error.issues.map((issue) => `${issue.path.join(".")}: ${issue.message}`).join("; "),
      },
      { status: 400 },
    )
  }
  console.error("bff internal error", error)
  return NextResponse.json(
    { code: "bff_error", message: "Internal error while talking to the Room Service." },
    { status: 500 },
  )
}

/**
 * Portal — Room Service upstream client (SERVER-SIDE ONLY).
 *
 * Used by the branding BFF route handler and the branded room page (server
 * component). Reads server-only environment and holds the service
 * credential; never import from client components.
 *
 * Environment: ROOM_SERVICE_URL (default http://localhost:8080),
 * ROOM_SERVICE_TOKEN — the Room Service dev shared secret
 * (EVDR_ROOM_SERVICE_DEV_SECRET). Production swaps the credential for the
 * service's OIDC wiring (TR-6.x).
 */
import { cache } from "react"

import { errorEnvelopeSchema, publicRoomSchema } from "./room-schemas"
import type { PublicRoom } from "./room-schemas"

export const ROOM_SERVICE_URL =
  process.env.ROOM_SERVICE_URL ?? "http://localhost:8080"

export class RoomServiceError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = "RoomServiceError"
    this.status = status
    this.code = code
  }
}

export async function publicRoomErrorFrom(
  response: Response,
): Promise<RoomServiceError> {
  try {
    const raw: unknown = await response.json()
    const parsed = errorEnvelopeSchema.safeParse(raw)
    if (parsed.success) {
      return new RoomServiceError(
        response.status,
        parsed.data.code,
        parsed.data.message,
      )
    }
  } catch {
    // Non-JSON body — fall through.
  }
  return new RoomServiceError(
    response.status,
    "upstream_error",
    `Room Service replied with HTTP ${response.status}`,
  )
}

async function fetchPublicRoom(roomId: string): Promise<Response> {
  const token = process.env.ROOM_SERVICE_TOKEN
  if (!token) {
    throw new Error(
      "ROOM_SERVICE_TOKEN is not set — the portal cannot authenticate to the Room Service. " +
        "See apps/portal/.env.example.",
    )
  }
  return fetch(
    `${ROOM_SERVICE_URL}/api/v1/public/rooms/${encodeURIComponent(roomId)}/branding`,
    {
      method: "GET",
      headers: { authorization: `Bearer ${token}` },
      cache: "no-store",
    },
  )
}

/**
 * Resolves the public branding of one room. Returns null when the room does
 * not exist (404) — the portal renders its honest "room not found" state.
 * Other upstream failures throw RoomServiceError.
 */
export async function getPublicRoomBranding(
  roomId: string,
): Promise<PublicRoom | null> {
  const response = await fetchPublicRoom(roomId)
  if (response.status === 404) {
    return null
  }
  if (!response.ok) {
    throw await publicRoomErrorFrom(response)
  }
  const raw: unknown = await response.json()
  const parsed = publicRoomSchema.safeParse(raw)
  if (!parsed.success) {
    throw new Error(
      `Room Service branding drifted from the contract: ${parsed.error.issues
        .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
        .join("; ")}`,
    )
  }
  return parsed.data
}

/**
 * Request-scoped memoisation: generateMetadata and the page both resolve the
 * same room, so the branding is fetched once per request.
 */
export const getPublicRoomBrandingCached = cache(async (roomId: string) =>
  getPublicRoomBranding(roomId),
)

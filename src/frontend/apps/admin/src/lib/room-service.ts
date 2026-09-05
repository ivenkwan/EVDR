/**
 * Admin console — Room Service upstream client (SERVER-SIDE ONLY).
 *
 * These helpers are imported exclusively by Next.js route handlers under
 * src/app/api/** — never by client components (they read server-only
 * environment and hold the service credential). The client talks only to the
 * same-origin BFF routes, which validate every upstream payload with the Zod
 * schemas in ./room-schemas.
 *
 * Environment (server): ROOM_SERVICE_URL (default http://localhost:8080),
 * ROOM_SERVICE_TOKEN — the dev shared secret the Room Service authenticates
 * (EVDR_ROOM_SERVICE_DEV_SECRET). Production replaces this credential with
 * the service's OIDC wiring (TR-6.x).
 */
import { z } from "zod"

import { errorEnvelopeSchema } from "./room-schemas"

export const ROOM_SERVICE_URL =
  process.env.ROOM_SERVICE_URL ?? "http://localhost:8080"

function roomServiceToken(): string {
  const token = process.env.ROOM_SERVICE_TOKEN
  if (!token) {
    throw new Error(
      "ROOM_SERVICE_TOKEN is not set — the BFF cannot authenticate to the Room Service. " +
        "See apps/admin/.env.example.",
    )
  }
  return token
}

/** Upstream error surfaced by the Room Service. */
export interface RoomServiceErrorPayload {
  code: string
  message: string
  requestId?: string
}

export class RoomServiceError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(status: number, payload: RoomServiceErrorPayload) {
    super(payload.message)
    this.name = "RoomServiceError"
    this.status = status
    this.code = payload.code
    this.requestId = payload.requestId
  }
}

async function parseErrorPayload(response: Response): Promise<RoomServiceErrorPayload> {
  try {
    const raw: unknown = await response.json()
    const parsed = errorEnvelopeSchema.safeParse(raw)
    if (parsed.success) {
      return parsed.data
    }
  } catch {
    // Non-JSON error body — fall through to defaults.
  }
  return { code: "upstream_error", message: `Room Service replied with HTTP ${response.status}` }
}

/** Builds a RoomServiceError from a non-2xx upstream response. */
export async function roomServiceErrorFrom(response: Response): Promise<RoomServiceError> {
  return new RoomServiceError(response.status, await parseErrorPayload(response))
}

/**
 * Calls the Room Service with auth attached and caching disabled. Lower-level
 * sibling of roomServiceJson for responses that are not JSON (uploads, tar
 * streams). Callers must handle non-2xx themselves.
 */
export async function roomServiceFetch(path: string, init: RequestInit): Promise<Response> {
  const { headers, body } = init
  return fetch(`${ROOM_SERVICE_URL}${path}`, {
    ...init,
    headers: {
      authorization: `Bearer ${roomServiceToken()}`,
      ...(body && !(body instanceof FormData) ? { "content-type": "application/json" } : {}),
      ...headers,
    },
    cache: "no-store",
  })
}

/**
 * Calls the Room Service with a Zod-validated response. Throws RoomServiceError
 * on non-2xx (payload already validated as the error envelope) and a ZodError
 * summary when the upstream contract drifts from ./room-schemas.
 */
export async function roomServiceJson<T>(
  path: string,
  init: RequestInit,
  schema: z.ZodType<T>,
): Promise<T> {
  const response = await roomServiceFetch(path, init)
  if (!response.ok) {
    throw await roomServiceErrorFrom(response)
  }
  const raw: unknown = await response.json()
  const parsed = schema.safeParse(raw)
  if (!parsed.success) {
    throw new Error(
      `Room Service response for ${path} drifted from the contract: ${parsed.error.issues
        .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
        .join("; ")}`,
    )
  }
  return parsed.data
}

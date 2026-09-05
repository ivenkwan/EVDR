/**
 * Admin console — typed gateway to the same-origin BFF routes under
 * /api/v1/rooms/**. All data flows through TanStack Query using these
 * functions; never bare fetch() in components (AGENTS.md §7).
 *
 * The BFF validates every Room Service payload with the wire schemas
 * (lib/room-schemas); these wrappers additionally parse so callers always
 * receive contract-typed data.
 */
import { z } from "zod"

import { API_BASE_URL, api, ApiError } from "@/lib/api"
import {
  createRoomRequestSchema,
  documentListSchema,
  documentSchema,
  grantListSchema,
  grantSchema,
  roomListSchema,
  roomSchema,
  sealReceiptSchema,
  versionListSchema,
  type CreateRoomRequest,
  type DocumentVersion,
  type Grant,
  type GrantRequest,
  type Room,
  type RoomDocument,
  type SealReceipt,
} from "@/lib/room-schemas"

/** Stable TanStack Query keys for room-service server state. */
export const roomKeys = {
  all: ["rooms"] as const,
  detail: (roomId: string) => ["rooms", roomId] as const,
  documents: (roomId: string) => ["rooms", roomId, "documents"] as const,
  versions: (roomId: string, documentId: string) =>
    ["rooms", roomId, "documents", documentId, "versions"] as const,
  grants: (roomId: string) => ["rooms", roomId, "grants"] as const,
}

async function parseOrThrow<T>(schema: z.ZodType<T>, data: unknown, what: string): Promise<T> {
  const parsed = schema.safeParse(data)
  if (!parsed.success) {
    throw new Error(
      `${what} drifted from the contract: ${parsed.error.issues
        .map((issue) => issue.message)
        .join("; ")}`,
    )
  }
  return parsed.data
}

export async function listRooms(): Promise<Room[]> {
  const data = await api.get<unknown>("/rooms")
  const parsed = await parseOrThrow(roomListSchema, data, "Room list")
  return parsed.rooms
}

export async function createRoom(request: CreateRoomRequest): Promise<Room> {
  const parsedBody = createRoomRequestSchema.safeParse(request)
  if (!parsedBody.success) {
    throw new ApiError(400, parsedBody.error.issues[0]?.message ?? "Invalid room", "bad_request")
  }
  const data = await api.post<unknown>("/rooms", parsedBody.data)
  return parseOrThrow(roomSchema, data, "Room")
}

export async function getRoom(roomId: string): Promise<Room> {
  const data = await api.get<unknown>(`/rooms/${encodeURIComponent(roomId)}`)
  return parseOrThrow(roomSchema, data, "Room")
}

export async function listDocuments(roomId: string): Promise<RoomDocument[]> {
  const data = await api.get<unknown>(`/rooms/${encodeURIComponent(roomId)}/documents`)
  const parsed = await parseOrThrow(documentListSchema, data, "Document list")
  return parsed.documents
}

export async function listVersions(roomId: string, documentId: string): Promise<DocumentVersion[]> {
  const data = await api.get<unknown>(
    `/rooms/${encodeURIComponent(roomId)}/documents/${encodeURIComponent(documentId)}/versions`,
  )
  const parsed = await parseOrThrow(versionListSchema, data, "Version list")
  return parsed.versions
}

export async function sealRoom(roomId: string, reason?: string): Promise<SealReceipt> {
  const data = await api.post<unknown>(
    `/rooms/${encodeURIComponent(roomId)}/seal`,
    { reason: reason ?? "" },
  )
  return parseOrThrow(sealReceiptSchema, data, "Seal receipt")
}

export async function listGrants(roomId: string): Promise<Grant[]> {
  const data = await api.get<unknown>(`/rooms/${encodeURIComponent(roomId)}/grants`)
  const parsed = await parseOrThrow(grantListSchema, data, "Grant list")
  return parsed.grants
}

export async function grantAccess(roomId: string, request: GrantRequest): Promise<Grant> {
  const data = await api.post<unknown>(
    `/rooms/${encodeURIComponent(roomId)}/grants`,
    request,
  )
  return parseOrThrow(grantSchema, data, "Grant")
}

export async function revokeGrant(roomId: string, grantId: string): Promise<void> {
  await api.del(`/rooms/${encodeURIComponent(roomId)}/grants/${encodeURIComponent(grantId)}`)
}

/** Same-origin URL that streams the room export tar (GET, triggers export). */
export function roomExportUrl(roomId: string): string {
  return `${API_BASE_URL}/rooms/${encodeURIComponent(roomId)}/export`
}

// ---------------------------------------------------------------------------
// Upload (FR-2.1): extension + size validation client-side, then multipart
// POST through the BFF with real progress (XHR upload events).
// ---------------------------------------------------------------------------

/** Document types accepted for upload (FR-2.1). */
export const ALLOWED_DOCUMENT_EXTENSIONS = [
  ".pdf",
  ".doc",
  ".docx",
  ".xls",
  ".xlsx",
  ".ppt",
  ".pptx",
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".tiff",
] as const

/** Client-side ceiling mirrors the Room Service default (100 MiB). */
export const MAX_UPLOAD_BYTES = 100 * 1024 * 1024

export interface UploadDocumentOptions {
  roomId: string
  file: File
  /** Explicit document name; defaults to the file name. */
  name?: string
  /** Slash-separated folder within the room (FR-2.3); "" = root. */
  folderPath?: string
  /** Classification label; empty = inherit the room floor. */
  classification?: string
  /** Progress callback with a 0–100 percent estimate. */
  onProgress?: (percent: number) => void
}

export function validateUploadFile(file: File): { ok: true } | { ok: false; error: string } {
  const lower = file.name.toLowerCase()
  const extensionOk = ALLOWED_DOCUMENT_EXTENSIONS.some((ext) => lower.endsWith(ext))
  if (!extensionOk) {
    return {
      ok: false,
      error: `Unsupported file type. Allowed: ${ALLOWED_DOCUMENT_EXTENSIONS.join(" ")}`,
    }
  }
  if (file.size === 0) {
    return { ok: false, error: "The file is empty." }
  }
  if (file.size > MAX_UPLOAD_BYTES) {
    return {
      ok: false,
      error: `The file is ${(file.size / (1024 * 1024)).toFixed(1)} MiB — the limit is 100 MiB.`,
    }
  }
  return { ok: true }
}

/**
 * Uploads a document with progress. Uses XMLHttpRequest because fetch() has
 * no upload-progress events. Progress covers the client → BFF leg; the BFF →
 * Room Service leg is reported as the final "storing" phase by the caller.
 */
export function uploadDocument(options: UploadDocumentOptions): Promise<RoomDocument> {
  const { roomId, file, onProgress } = options
  const form = new FormData()
  form.append("file", file, file.name)
  if (options.name && options.name.trim() !== "") form.append("name", options.name.trim())
  if (options.folderPath && options.folderPath.trim() !== "")
    form.append("folderPath", options.folderPath.trim())
  if (options.classification && options.classification !== "")
    form.append("classification", options.classification)

  return new Promise<RoomDocument>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open("POST", `${API_BASE_URL}/rooms/${encodeURIComponent(roomId)}/documents`)
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && onProgress) {
        const percent = Math.min(99, Math.round((event.loaded / event.total) * 100))
        onProgress(percent)
      }
    }
    xhr.onload = () => {
      let payload: unknown
      try {
        payload = JSON.parse(xhr.responseText)
      } catch {
        reject(new ApiError(xhr.status, "Upload failed — unexpected server response"))
        return
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        const parsed = documentSchema.safeParse(payload)
        if (!parsed.success) {
          reject(new ApiError(xhr.status, "Upload response drifted from the contract"))
          return
        }
        onProgress?.(100)
        resolve(parsed.data)
        return
      }
      const message =
        payload && typeof payload === "object" && "message" in payload
          ? String((payload as { message: unknown }).message)
          : `Upload failed with status ${xhr.status}`
      const code =
        payload && typeof payload === "object" && "code" in payload
          ? String((payload as { code: unknown }).code)
          : "upload_failed"
      reject(new ApiError(xhr.status, message, code))
    }
    xhr.onerror = () => reject(new ApiError(0, "Network error during upload"))
    xhr.onabort = () => reject(new ApiError(0, "Upload aborted"))
    xhr.send(form)
  })
}

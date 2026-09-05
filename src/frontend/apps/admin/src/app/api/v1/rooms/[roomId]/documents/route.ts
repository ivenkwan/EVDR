/**
 * BFF: room documents (FR-2.1 upload / FR-2.2 version tracking).
 *
 * GET  — document list (current-version metadata per document).
 * POST — multipart upload: forwards the client FormData to the Room Service
 *        (PutDocument). The service caps the body size authoritatively.
 *
 * The upload leg uses node:http instead of fetch(): Next's instrumented
 * fetch detaches large request bodies on this stack, and re-serialising
 * Next's FormData/File objects produced empty parts. Frames the multipart
 * body by hand (whitelisted fields, CR/LF-stripped values, quoted
 * filenames) and streams it with an explicit content-length.
 */
import { randomUUID } from "node:crypto"
import { Buffer } from "node:buffer"
import { request as httpRequest } from "node:http"
import { URL } from "node:url"

import { NextResponse } from "next/server"

import { bffError, idParamSchema, roomServicePath } from "@/lib/bff"
import { documentListSchema, documentSchema, errorEnvelopeSchema } from "@/lib/room-schemas"
import {
  RoomServiceError,
  ROOM_SERVICE_URL,
  roomServiceErrorFrom,
  roomServiceFetch,
} from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RouteContext {
  params: Promise<{ roomId: string }>
}

function parseRoomId(raw: string): string | null {
  const parsed = idParamSchema.safeParse(raw)
  return parsed.success ? parsed.data : null
}

function serviceToken(): string {
  const token = process.env.ROOM_SERVICE_TOKEN
  if (!token) {
    throw new Error(
      "ROOM_SERVICE_TOKEN is not set — the BFF cannot authenticate to the Room Service. " +
        "See apps/admin/.env.example.",
    )
  }
  return token
}

/** Frames a multipart/form-data body by hand (see file header comment). */
async function frameMultipart(
  file: File,
  fields: Record<string, string>,
): Promise<{ body: Buffer; contentType: string }> {
  const boundary = `----EVDR-BFF-${randomUUID()}`
  const crlf = "\r\n"
  const chunks: Buffer[] = []

  const pushText = (text: string) => chunks.push(Buffer.from(text, "utf8"))

  for (const [name, value] of Object.entries(fields)) {
    if (value === "") continue
    pushText(
      `--${boundary}${crlf}content-disposition: form-data; name="${name}"${crlf}${crlf}${value}${crlf}`,
    )
  }
  // Filename is quoted with JSON.stringify — CR/LF and quotes cannot break
  // out of the header.
  pushText(
    `--${boundary}${crlf}content-disposition: form-data; name="file"; filename=${JSON.stringify(file.name)}${crlf}` +
      `content-type: ${file.type || "application/octet-stream"}${crlf}${crlf}`,
  )
  chunks.push(Buffer.from(await file.arrayBuffer()))
  pushText(`${crlf}--${boundary}--${crlf}`)
  return { body: Buffer.concat(chunks), contentType: `multipart/form-data; boundary=${boundary}` }
}

/** node:http upload — raw control, no instrumented fetch in the path. */
function uploadMultipart(path: string, contentType: string, body: Buffer): Promise<{
  status: number
  text: string
}> {
  const token = serviceToken()
  const url = new URL(`${ROOM_SERVICE_URL}${path}`)
  return new Promise((resolve, reject) => {
    const req = httpRequest(
      url,
      {
        method: "POST",
        headers: {
          authorization: `Bearer ${token}`,
          "content-type": contentType,
          "content-length": String(body.byteLength),
        },
      },
      (res) => {
        const chunks: Buffer[] = []
        res.on("data", (chunk: Buffer) => chunks.push(chunk))
        res.on("end", () => {
          resolve({ status: res.statusCode ?? 0, text: Buffer.concat(chunks).toString("utf8") })
        })
        res.on("error", reject)
      },
    )
    req.on("error", reject)
    req.end(body)
  })
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const id = parseRoomId(roomId)
    if (!id) {
      return NextResponse.json({ code: "bad_request", message: "invalid roomId" }, { status: 400 })
    }
    const response = await roomServiceFetch(
      roomServicePath("/rooms", encodeURIComponent(id), "/documents"),
      { method: "GET" },
    )
    if (!response.ok) {
      throw await roomServiceErrorFrom(response)
    }
    const parsed = documentListSchema.safeParse(await response.json())
    if (!parsed.success) {
      throw new Error(
        `Room Service response drifted from the contract: ${parsed.error.issues
          .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
          .join("; ")}`,
      )
    }
    return NextResponse.json(parsed.data)
  } catch (error) {
    return bffError(error)
  }
}

export async function POST(request: Request, context: RouteContext) {
  try {
    const { roomId } = await context.params
    const id = parseRoomId(roomId)
    if (!id) {
      return NextResponse.json({ code: "bad_request", message: "invalid roomId" }, { status: 400 })
    }
    const clientForm = await request.formData()
    const file = clientForm.get("file")
    if (!(file instanceof File) || !file.name) {
      return NextResponse.json(
        { code: "bad_request", message: "a 'file' part is required" },
        { status: 400 },
      )
    }
    // Forward name/folderPath/classification when the client set them; the
    // Room Service defaults the name to the file name otherwise.
    const fields: Record<string, string> = {}
    for (const field of ["name", "folderPath", "classification"] as const) {
      const value = clientForm.get(field)
      if (typeof value === "string" && value !== "") {
        fields[field] = value.replace(/[\r\n]/g, "")
      }
    }
    const { body, contentType } = await frameMultipart(file, fields)
    const upstream = await uploadMultipart(
      roomServicePath("/rooms", encodeURIComponent(id), "/documents"),
      contentType,
      body,
    )

    let payload: unknown = null
    try {
      payload = JSON.parse(upstream.text)
    } catch {
      // Non-JSON body — handled below.
    }
    if (upstream.status < 200 || upstream.status >= 300) {
      const envelope = errorEnvelopeSchema.safeParse(payload)
      if (envelope.success) {
        throw new RoomServiceError(upstream.status, envelope.data)
      }
      throw new RoomServiceError(upstream.status, {
        code: "upstream_error",
        message: `Room Service replied with HTTP ${upstream.status}`,
      })
    }
    const parsed = documentSchema.safeParse(payload)
    if (!parsed.success) {
      throw new Error(
        `Room Service response drifted from the contract: ${parsed.error.issues
          .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
          .join("; ")}`,
      )
    }
    return NextResponse.json(parsed.data, { status: 201 })
  } catch (error) {
    return bffError(error)
  }
}

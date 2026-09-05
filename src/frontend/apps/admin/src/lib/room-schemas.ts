/**
 * Admin console — Room Service wire schemas.
 *
 * Zod schemas for every Room Service (/api/v1) response and request body,
 * mirroring the Go wire DTOs in src/services/room/dto.go. They are the typed
 * contract between the admin BFF (validation boundary) and the UI; the BFF
 * never forwards an unvalidated Room Service payload to the client.
 *
 * Time fields are RFC 3339 strings (Go time.Time JSON), e.g.
 * "2026-09-05T02:43:15.447725077Z".
 */
import { z } from "zod"

export const isoDatetime = z.string().datetime({ offset: true })

export const actorSchema = z.object({
  kind: z.string(),
  id: z.string(),
  displayName: z.string(),
})

export const brandingSchema = z.object({
  logoObjectKey: z.string().optional(),
  theme: z.record(z.string(), z.string()).optional(),
  aboutPage: z.string().optional(),
})

export const retentionSchema = z.object({
  anchor: z.string(),
  minRetentionDays: z.number().int(),
  maxRetentionDays: z.number().int(),
  autoPurge: z.boolean(),
})

export const roomSchema = z.object({
  id: z.string().min(1),
  name: z.string(),
  slug: z.string(),
  state: z.enum(["active", "sealed", "closed", "archived"]),
  branding: brandingSchema,
  classification: z.string(),
  retention: retentionSchema,
  createdAt: isoDatetime,
  createdBy: actorSchema,
  closedAt: isoDatetime.nullable().optional(),
})

export const roomListSchema = z.object({
  rooms: z.array(roomSchema),
})

export const createRoomRequestSchema = z.object({
  name: z.string().min(1).max(200),
  slug: z
    .string()
    .min(1)
    .max(64)
    .regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/, "lowercase letters, digits and hyphens"),
  branding: brandingSchema.optional(),
  classification: z.enum(["", "PUBLIC", "INTERNAL", "CONFIDENTIAL", "RESTRICTED"]).optional(),
  retention: retentionSchema.optional(),
})

export const sealRequestSchema = z.object({
  reason: z.string().optional(),
  legalHoldRef: z.string().optional(),
})

export const sealReceiptSchema = z.object({
  id: z.string().min(1),
  roomId: z.string().min(1),
  sealedAt: isoDatetime,
  sealedBy: actorSchema,
  frozenObjects: z.number().int(),
})

export const documentSchema = z.object({
  id: z.string().min(1),
  roomId: z.string().min(1),
  name: z.string(),
  folderPath: z.string(),
  contentType: z.string(),
  classification: z.string(),
  currentVersion: z.string(),
  sizeBytes: z.number().int().nonnegative(),
  uploadedAt: isoDatetime,
  uploadedBy: actorSchema,
})

export const documentListSchema = z.object({
  documents: z.array(documentSchema),
})

export const versionSchema = z.object({
  id: z.string().min(1),
  documentId: z.string().min(1),
  number: z.number().int().positive(),
  sha256: z.string(),
  sizeBytes: z.number().int().nonnegative(),
  createdAt: isoDatetime,
  createdBy: actorSchema,
})

export const versionListSchema = z.object({
  versions: z.array(versionSchema),
})

export const grantConstraintsSchema = z.object({
  notBefore: isoDatetime.nullable().optional(),
  notAfter: isoDatetime.nullable().optional(),
  cidrs: z.array(z.string()).optional(),
  domains: z.array(z.string()).optional(),
})

export const grantSchema = z.object({
  id: z.string().min(1),
  subject: z.string(),
  actorKind: z.enum(["internal_user", "guest", "service"]),
  tier: z.enum(["view_only", "download_allowed", "print_allowed", "edit_allowed"]),
  constraints: grantConstraintsSchema,
  createdAt: isoDatetime,
  createdBy: actorSchema,
})

export const grantListSchema = z.object({
  grants: z.array(grantSchema),
})

export const grantRequestSchema = z.object({
  subject: z.string().min(1).max(320),
  actorKind: z.enum(["internal_user", "guest", "service"]),
  tier: z.enum(["view_only", "download_allowed", "print_allowed", "edit_allowed"]).optional(),
  constraints: grantConstraintsSchema.optional(),
})

export const errorEnvelopeSchema = z.object({
  code: z.string(),
  message: z.string(),
  requestId: z.string().optional(),
})

// ---- Inferred types ------------------------------------------------------

export type Room = z.infer<typeof roomSchema>
export type RoomBranding = z.infer<typeof brandingSchema>
export type RoomDocument = z.infer<typeof documentSchema>
export type DocumentVersion = z.infer<typeof versionSchema>
export type Grant = z.infer<typeof grantSchema>
export type SealReceipt = z.infer<typeof sealReceiptSchema>
export type CreateRoomRequest = z.infer<typeof createRoomRequestSchema>
export type GrantRequest = z.infer<typeof grantRequestSchema>

/**
 * Portal — Room Service wire schemas (public surface only).
 *
 * The external portal can only ever see the public room branding projection
 * (FR-1.1): name, slug, state and branding. No documents, grants, retention,
 * creator or audit data crosses this boundary — the schemas below are the
 * smallest honest slice of the Room Service wire contract (dto.go) the guest
 * needs.
 */
import { z } from "zod"

export const publicBrandingSchema = z.object({
  logoObjectKey: z.string().optional(),
  theme: z.record(z.string(), z.string()).optional(),
  aboutPage: z.string().optional(),
})

export const publicRoomSchema = z.object({
  id: z.string().min(1),
  name: z.string(),
  slug: z.string(),
  state: z.enum(["active", "sealed", "closed", "archived"]),
  branding: publicBrandingSchema,
})

export const errorEnvelopeSchema = z.object({
  code: z.string(),
  message: z.string(),
  requestId: z.string().optional(),
})

export type PublicRoom = z.infer<typeof publicRoomSchema>
export type PublicRoomBranding = z.infer<typeof publicBrandingSchema>

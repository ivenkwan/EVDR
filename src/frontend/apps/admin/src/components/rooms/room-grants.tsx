"use client"

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"

import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { KeyRound, Loader2, ShieldX } from "lucide-react"

import { Field, inputClass, selectClass } from "@/components/rooms/form-controls"
import { ApiError } from "@/lib/api"
import { formatDateTime, humanize } from "@/lib/format"
import { grantAccess, listGrants, revokeGrant, roomKeys } from "@/lib/rooms-api"
import type { Grant } from "@/lib/room-schemas"

function fmtConstraint(iso: string | null | undefined, label: string): string | null {
  if (!iso) return null
  return `${label} ${formatDateTime(iso)}`
}

/**
 * Access grants for the room (FR-4.4 lifecycle through the Room Service).
 * Guest grants must carry an expiry — enforced by the Room Service (SPI
 * ErrInvalidGrant) and pre-checked here.
 */
export function RoomGrants({ roomId, sealed }: { roomId: string; sealed: boolean }) {
  const queryClient = useQueryClient()

  const grantsQuery = useQuery({
    queryKey: roomKeys.grants(roomId),
    queryFn: () => listGrants(roomId),
  })

  const [subject, setSubject] = useState("")
  const [kind, setKind] = useState<"internal_user" | "guest" | "service">("internal_user")
  const [tier, setTier] = useState<"view_only" | "download_allowed" | "print_allowed" | "edit_allowed">(
    "view_only",
  )
  const [expiresAt, setExpiresAt] = useState("")
  const [formError, setFormError] = useState<string | null>(null)

  const grantMutation = useMutation({
    mutationFn: () => {
      const expiry = expiresAt !== "" ? new Date(expiresAt) : null
      if (kind === "guest" && !expiry) {
        return Promise.reject(
          new ApiError(400, "Guest grants must include an expiry date and time.", "invalid_grant"),
        )
      }
      if (expiry && Number.isNaN(expiry.getTime())) {
        return Promise.reject(new ApiError(400, "The expiry date is not valid.", "invalid_grant"))
      }
      return grantAccess(roomId, {
        subject: subject.trim(),
        actorKind: kind,
        tier,
        ...(expiry ? { constraints: { notAfter: expiry.toISOString() } } : {}),
      })
    },
    onSuccess: () => {
      setSubject("")
      setExpiresAt("")
      setKind("internal_user")
      setTier("view_only")
      void queryClient.invalidateQueries({ queryKey: roomKeys.grants(roomId) })
    },
    onError: (error) => setFormError(error instanceof ApiError ? error.message : "Grant failed."),
  })

  const revokeMutation = useMutation({
    mutationFn: (grantId: string) => revokeGrant(roomId, grantId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: roomKeys.grants(roomId) }),
  })

  const disabled = sealed

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4 text-primary" aria-hidden />
          Access grants
        </CardTitle>
        <CardDescription>
          Who can reach the room and at which permission tier (FR-4.4). Guests
          always get an expiring grant.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <form
          onSubmit={(event) => {
            event.preventDefault()
            setFormError(null)
            grantMutation.mutate()
          }}
          className="space-y-4 rounded-md border bg-muted/40 p-4"
        >
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Field label="Subject" htmlFor="grant-subject" hint="Email or principal ID.">
              <input
                id="grant-subject"
                className={inputClass}
                value={subject}
                onChange={(event) => setSubject(event.target.value)}
                placeholder="auditor@acme.com"
                required
                disabled={disabled || grantMutation.isPending}
              />
            </Field>
            <Field label="Actor kind" htmlFor="grant-kind">
              <select
                id="grant-kind"
                className={selectClass}
                value={kind}
                onChange={(event) => setKind(event.target.value as typeof kind)}
                disabled={disabled || grantMutation.isPending}
              >
                <option value="internal_user">Internal user</option>
                <option value="guest">Guest</option>
                <option value="service">Service</option>
              </select>
            </Field>
            <Field label="Tier" htmlFor="grant-tier">
              <select
                id="grant-tier"
                className={selectClass}
                value={tier}
                onChange={(event) => setTier(event.target.value as typeof tier)}
                disabled={disabled || grantMutation.isPending}
              >
                <option value="view_only">View only</option>
                <option value="download_allowed">Download allowed</option>
                <option value="print_allowed">Print allowed</option>
                <option value="edit_allowed">Edit allowed</option>
              </select>
            </Field>
            <Field label="Expires at (guests)" htmlFor="grant-expiry">
              <input
                id="grant-expiry"
                type="datetime-local"
                className={inputClass}
                value={expiresAt}
                onChange={(event) => setExpiresAt(event.target.value)}
                disabled={disabled || grantMutation.isPending}
              />
            </Field>
          </div>
          {formError ? (
            <p className="text-sm text-destructive" role="alert">
              {formError}
            </p>
          ) : null}
          <div className="flex justify-end">
            <Button type="submit" disabled={disabled || grantMutation.isPending}>
              {grantMutation.isPending ? (
                <Loader2 className="animate-spin" aria-hidden />
              ) : null}
              Grant access
            </Button>
          </div>
          {disabled ? (
            <p className="text-xs text-muted-foreground">
              This room is sealed — access can no longer be granted (SPI
              ErrRoomSealed). Revocation stays available for safety.
            </p>
          ) : null}
        </form>

        {grantsQuery.isPending ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" aria-hidden />
            Loading grants…
          </p>
        ) : grantsQuery.isError ? (
          <p className="text-sm text-destructive">
            Could not load grants: {grantsQuery.error.message}
          </p>
        ) : grantsQuery.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">No active grants.</p>
        ) : (
          <ul className="divide-y">
            {grantsQuery.data.map((grant) => (
              <GrantRow
                key={grant.id}
                grant={grant}
                revoking={revokeMutation.isPending}
                onRevoke={() => {
                  void revokeMutation.mutate(grant.id)
                }}
              />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function GrantRow({
  grant,
  revoking,
  onRevoke,
}: {
  grant: Grant
  revoking: boolean
  onRevoke: () => void
}) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">
          {grant.subject}
          <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs font-normal text-muted-foreground">
            {grant.actorKind}
          </span>
        </p>
        <p className="text-xs text-muted-foreground">
          {humanize(grant.tier)}
          {fmtConstraint(grant.constraints?.notAfter ?? null, "· expires")}
          {fmtConstraint(grant.constraints?.notBefore ?? null, "· starts")}
          {grant.constraints?.domains?.length
            ? ` · domains ${grant.constraints.domains.join(", ")}`
            : ""}
        </p>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onRevoke}
        disabled={revoking}
        aria-label={`Revoke access for ${grant.subject}`}
      >
        {revoking ? <Loader2 className="size-3 animate-spin" aria-hidden /> : <ShieldX className="size-3" aria-hidden />}
        Revoke
      </Button>
    </li>
  )
}

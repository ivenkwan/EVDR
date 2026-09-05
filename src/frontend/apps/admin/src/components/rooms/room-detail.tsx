"use client"

import Link from "next/link"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"

import { Button, Card, CardContent } from "@evdr/ui"
import { Archive, ArrowLeft, Loader2, ShieldAlert, ShieldCheck } from "lucide-react"

import { RoomDocuments } from "@/components/rooms/room-documents"
import { RoomGrants } from "@/components/rooms/room-grants"
import { UploadDocument } from "@/components/rooms/upload-document"
import { ApiError } from "@/lib/api"
import { formatDateTime, humanize } from "@/lib/format"
import { getRoom, roomExportUrl, roomKeys, sealRoom } from "@/lib/rooms-api"
import type { SealReceipt } from "@/lib/room-schemas"

/**
 * Room detail: documents + versions (FR-2.2), upload (FR-2.1), grants
 * (FR-4.4), seal (FR-1.6) and export (FR-1.7) — all through the BFF.
 */
export function RoomDetail({ roomId }: { roomId: string }) {
  const queryClient = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)
  const [sealReceipt, setSealReceipt] = useState<SealReceipt | null>(null)

  const roomQuery = useQuery({
    queryKey: roomKeys.detail(roomId),
    queryFn: () => getRoom(roomId),
  })

  const sealMutation = useMutation({
    mutationFn: () => sealRoom(roomId, "Requested from the admin console"),
    onSuccess: (receipt) => {
      setSealReceipt(receipt)
      void queryClient.invalidateQueries({ queryKey: roomKeys.detail(roomId) })
    },
    onError: (error) => setActionError(error instanceof ApiError ? error.message : "Seal failed."),
  })

  function onSealClick() {
    // Sealing is a legal hold (FR-1.6) — confirm explicitly before acting.
    if (
      globalThis.confirm(
        "Seal this room? It becomes immutable: no more uploads, grants or retention changes. Reads and export stay available.",
      )
    ) {
      sealMutation.mutate()
    }
  }

  if (roomQuery.isPending) {
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" aria-hidden />
        Loading room…
      </p>
    )
  }
  if (roomQuery.isError) {
    return (
      <Card>
        <CardContent className="flex items-center gap-2 py-6 text-sm text-destructive">
          <ShieldAlert className="size-4" aria-hidden />
          Could not load the room: {roomQuery.error.message}
        </CardContent>
      </Card>
    )
  }
  const room = roomQuery.data
  const sealed = room.state === "sealed"

  return (
    <div className="space-y-6">
      <div>
        <Link
          href="/rooms"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" aria-hidden />
          Rooms
        </Link>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight">{room.name}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          /rooms/{room.slug} · <span className="font-mono">{room.id}</span>
        </p>
      </div>

      <Card>
        <CardContent className="flex flex-wrap items-center justify-between gap-4 py-4">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
            <span>
              State:{" "}
              <span className="font-medium text-foreground">{humanize(room.state)}</span>
            </span>
            <span>Classification: {room.classification}</span>
            {room.retention.minRetentionDays > 0 ? (
              <span>
                Retention floor: {room.retention.minRetentionDays} days
              </span>
            ) : (
              <span>Retention: none set</span>
            )}
            <span>Created {formatDateTime(room.createdAt)}</span>
          </div>
          <div className="flex gap-2">
            <Button asChild variant="outline" size="sm">
              <a href={roomExportUrl(roomId)}>
                <Archive className="size-3.5" aria-hidden />
                Export (.tar)
              </a>
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={sealed}
              onClick={onSealClick}
            >
              {sealMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" aria-hidden />
              ) : null}
              {sealed ? "Sealed" : "Seal room"}
            </Button>
          </div>
        </CardContent>
      </Card>

      {sealed || sealReceipt ? (
        <div
          className="flex items-start gap-2 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900"
          role="status"
        >
          <ShieldCheck className="mt-0.5 size-4 shrink-0" aria-hidden />
          <p>
            This room is under legal hold (sealed {sealReceipt ? formatDateTime(sealReceipt.sealedAt) : "at an earlier time"}).
            All mutations are rejected by the Room Service; reads and export remain available.
          </p>
        </div>
      ) : null}

      {actionError ? (
        <p className="text-sm text-destructive" role="alert">
          {actionError}
        </p>
      ) : null}

      <UploadDocument roomId={roomId} />
      <RoomDocuments roomId={roomId} />
      <RoomGrants roomId={roomId} sealed={sealed} />
    </div>
  )
}

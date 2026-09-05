"use client"

import { useQuery } from "@tanstack/react-query"
import { useState } from "react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { ChevronDown, ChevronRight, FileText, Loader2 } from "lucide-react"

import { formatBytes, formatDateTime, humanize, shortDigest } from "@/lib/format"
import { listDocuments, listVersions, roomKeys } from "@/lib/rooms-api"
import type { DocumentVersion, RoomDocument } from "@/lib/room-schemas"

/**
 * Document list with expandable immutable version history (FR-2.2). Version
 * history is a live SPI read (ListVersions) fetched on first expand — the
 * Room Service never serves it from a projection.
 */
export function RoomDocuments({ roomId }: { roomId: string }) {
  const [expanded, setExpanded] = useState<string | null>(null)

  const documentsQuery = useQuery({
    queryKey: roomKeys.documents(roomId),
    queryFn: () => listDocuments(roomId),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Documents</CardTitle>
        <CardDescription>
          Uploaded via the Room Service; each upload creates an immutable
          version (FR-2.2).
        </CardDescription>
      </CardHeader>
      <CardContent>
        {documentsQuery.isPending ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" aria-hidden />
            Loading documents…
          </p>
        ) : documentsQuery.isError ? (
          <p className="text-sm text-destructive">
            Could not load documents: {documentsQuery.error.message}
          </p>
        ) : documentsQuery.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No documents yet. Use the upload card to add the first one.
          </p>
        ) : (
          <ul className="divide-y">
            {documentsQuery.data.map((document) => (
              <DocumentRow
                key={document.id}
                roomId={roomId}
                document={document}
                open={expanded === document.id}
                onToggle={() =>
                  setExpanded((current) =>
                    current === document.id ? null : document.id,
                  )
                }
              />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function DocumentRow({
  roomId,
  document,
  open,
  onToggle,
}: {
  roomId: string
  document: RoomDocument
  open: boolean
  onToggle: () => void
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-3 py-3 text-left"
        aria-expanded={open}
      >
        <FileText className="size-4 shrink-0 text-primary" aria-hidden />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">{document.name}</span>
          <span className="block text-xs text-muted-foreground">
            {document.folderPath !== "" ? `${document.folderPath} · ` : ""}
            {humanize(document.classification)} · {formatBytes(document.sizeBytes)} ·{" "}
            {formatDateTime(document.uploadedAt)}
          </span>
        </span>
        {open ? (
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        ) : (
          <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        )}
      </button>
      {open ? <VersionHistory roomId={roomId} documentId={document.id} /> : null}
    </li>
  )
}

function VersionHistory({ roomId, documentId }: { roomId: string; documentId: string }) {
  const versionsQuery = useQuery({
    queryKey: roomKeys.versions(roomId, documentId),
    queryFn: () => listVersions(roomId, documentId),
  })

  if (versionsQuery.isPending) {
    return (
      <p className="flex items-center gap-2 pb-3 pl-7 text-xs text-muted-foreground">
        <Loader2 className="size-3 animate-spin" aria-hidden />
        Loading version history…
      </p>
    )
  }
  if (versionsQuery.isError) {
    return (
      <p className="pb-3 pl-7 text-xs text-destructive">
        Versions unavailable: {versionsQuery.error.message}
      </p>
    )
  }
  const versions = versionsQuery.data
  return (
    <div className="mb-3 ml-4 rounded-md border bg-muted/40 p-3">
      <p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Versions ({versions.length})
      </p>
      <ul className="space-y-1.5">
        {versions.map((version) => (
          <VersionRow key={version.id} version={version} latest={version === versions.at(-1)} />
        ))}
      </ul>
    </div>
  )
}

function VersionRow({ version, latest }: { version: DocumentVersion; latest: boolean }) {
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs">
      <span className="inline-flex items-center gap-1.5 font-medium">
        v{version.number}
        {latest ? (
          <span className="rounded-full bg-primary px-1.5 py-0.5 text-[10px] text-primary-foreground">
            current
          </span>
        ) : null}
      </span>
      <span className="font-mono text-muted-foreground">{shortDigest(version.sha256)}</span>
      <span className="text-muted-foreground">{formatBytes(version.sizeBytes)}</span>
      <span className="text-muted-foreground">{formatDateTime(version.createdAt)}</span>
    </li>
  )
}

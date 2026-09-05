"use client"

import Link from "next/link"
import { useQuery } from "@tanstack/react-query"
import { useState } from "react"

import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { FolderLock, Loader2, Plus, ShieldAlert } from "lucide-react"

import { CreateRoomForm } from "@/components/rooms/create-room-form"
import { formatDateTime, humanize } from "@/lib/format"
import { listRooms, roomKeys } from "@/lib/rooms-api"

function StateChip({ state }: { state: string }) {
  const tone =
    state === "active"
      ? "bg-emerald-100 text-emerald-800"
      : state === "sealed"
        ? "bg-amber-100 text-amber-800"
        : "bg-muted text-muted-foreground"
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${tone}`}
    >
      {humanize(state)}
    </span>
  )
}

/**
 * Rooms list + create (admin console, wired to the Room Service via BFF).
 * Rooms come from the service projection of SPI-confirmed records — the
 * frozen SPI has no enumeration method.
 */
export function RoomsManager() {
  const [creating, setCreating] = useState(false)

  const roomsQuery = useQuery({
    queryKey: roomKeys.all,
    queryFn: () => listRooms(),
  })

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Rooms</h1>
          <p className="mt-1 text-muted-foreground">
            Data rooms provisioned through the Room Service (SPI CreateRoom).
          </p>
        </div>
        <Button variant="outline" onClick={() => setCreating((open) => !open)}>
          <Plus className="size-4" aria-hidden />
          {creating ? "Close form" : "New room"}
        </Button>
      </div>

      {creating ? (
        <Card>
          <CardHeader>
            <CardTitle>New room</CardTitle>
            <CardDescription>
              Branding fields (FR-1.1): name, slug, accent colour and About text
              today; logo assets arrive with the portal asset pipeline.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <CreateRoomForm />
          </CardContent>
        </Card>
      ) : null}

      {roomsQuery.isPending ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" aria-hidden />
          Loading rooms…
        </div>
      ) : roomsQuery.isError ? (
        <Card>
          <CardContent className="flex items-center gap-2 py-6 text-sm text-destructive">
            <ShieldAlert className="size-4" aria-hidden />
            Could not load rooms: {roomsQuery.error.message}. Is the Room
            Service reachable (ROOM_SERVICE_URL)?
          </CardContent>
        </Card>
      ) : roomsQuery.data.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FolderLock className="size-4 text-primary" aria-hidden />
              No rooms yet
            </CardTitle>
            <CardDescription>
              Create a room to start exchanging documents under governance.
            </CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Room creation provisions storage, metadata and the initial
            retention policy through the Room Service.
          </CardContent>
        </Card>
      ) : (
        <ul className="space-y-3">
          {roomsQuery.data.map((room) => (
            <li key={room.id}>
              <Link href={`/rooms/${encodeURIComponent(room.id)}`} className="block">
                <Card className="transition-colors hover:border-primary/50">
                  <CardContent className="flex flex-wrap items-center justify-between gap-3 py-4">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="truncate font-medium">{room.name}</p>
                        <StateChip state={room.state} />
                      </div>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        /rooms/{room.slug} · {room.classification} · created{" "}
                        {formatDateTime(room.createdAt)}
                      </p>
                    </div>
                    <span className="font-mono text-xs text-muted-foreground">
                      {room.id.slice(0, 8)}…
                    </span>
                  </CardContent>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

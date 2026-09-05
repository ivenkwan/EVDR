import type { Metadata } from "next"
import Link from "next/link"
import { notFound } from "next/navigation"
import type { CSSProperties } from "react"

import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { LockKeyhole, ShieldCheck, TriangleAlert } from "lucide-react"

import { getPublicRoomBrandingCached } from "@/lib/room-service"

export const dynamic = "force-dynamic"

interface RoomPageProps {
  params: Promise<{ roomId: string }>
}

export async function generateMetadata({ params }: RoomPageProps): Promise<Metadata> {
  const { roomId } = await params
  try {
    const room = await getPublicRoomBrandingCached(roomId)
    if (room) {
      return { title: room.name }
    }
  } catch {
    // The error state is rendered on the page itself.
  }
  return { title: `Room ${roomId}` }
}

function RoomUnavailable() {
  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-12 sm:px-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <TriangleAlert className="size-4 text-amber-600" aria-hidden />
            Room unavailable
          </CardTitle>
          <CardDescription>
            The room service could not be reached right now. Please try again
            shortly.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button asChild variant="outline" size="sm">
            <Link href="/rooms">Back to data rooms</Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

/**
 * Branded external room page (FR-1.1, TR-3.1 SSR). Server component:
 * branding is resolved through the portal server layer (no-store) and
 * rendered with the room's theme CSS variables applied locally. The guest
 * sees ONLY the public projection — no documents, no grants, no management
 * data. The NDA/consent gate and secure viewer arrive in later waves
 * (FR-1.x/TR-4.x).
 */
export default async function RoomPage({ params }: RoomPageProps) {
  const { roomId } = await params
  let room: Awaited<ReturnType<typeof getPublicRoomBrandingCached>> | null = null
  let unreachable = false
  try {
    room = await getPublicRoomBrandingCached(roomId)
  } catch {
    unreachable = true
  }
  if (unreachable) {
    return <RoomUnavailable />
  }
  if (!room) {
    notFound()
  }

  const theme = (room.branding.theme ?? {}) as CSSProperties
  const inaccessible = room.state !== "active"
  const monogram = room.name.trim().charAt(0).toUpperCase() || "R"

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-12 sm:px-6">
      <section style={theme} className="overflow-hidden rounded-xl border bg-card shadow-sm">
        <div className="h-1.5 bg-primary" aria-hidden />
        <div className="flex flex-wrap items-center gap-4 p-6 sm:p-8">
          <div
            className="flex size-14 items-center justify-center rounded-lg bg-primary text-lg font-semibold text-primary-foreground"
            aria-hidden
          >
            {room.branding.logoObjectKey ? <ShieldCheck className="size-7" /> : monogram}
          </div>
          <div className="min-w-0">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Invitation-only data room
            </p>
            <h1 className="truncate text-2xl font-semibold tracking-tight sm:text-3xl">
              {room.name}
            </h1>
          </div>
        </div>

        {inaccessible ? (
          <div
            className="mx-6 mb-6 flex items-start gap-2 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 sm:mx-8"
            role="status"
          >
            <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
            <p>
              This room is not currently accepting access. Contact your host if
              you expected it to be open.
            </p>
          </div>
        ) : null}

        {room.branding.aboutPage ? (
          <div className="border-t px-6 py-6 sm:px-8">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              About this room
            </h2>
            <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed">
              {room.branding.aboutPage}
            </p>
          </div>
        ) : null}
      </section>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <LockKeyhole className="size-4 text-primary" aria-hidden />
            Room access
          </CardTitle>
          <CardDescription>
            This room is protected. The host shares a secure invitation when
            documents are ready for you.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Document access is enforced server-side, watermarking is applied
          server-side, and every page view is audit-logged (FR-1.x, TR-4.x).
        </CardContent>
      </Card>
    </div>
  )
}

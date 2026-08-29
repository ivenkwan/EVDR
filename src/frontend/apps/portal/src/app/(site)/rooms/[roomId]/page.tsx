import type { Metadata } from "next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { LockKeyhole, ShieldCheck } from "lucide-react"

interface RoomPageProps {
  params: Promise<{ roomId: string }>
}

export async function generateMetadata({ params }: RoomPageProps): Promise<Metadata> {
  const { roomId } = await params
  return { title: `Room ${roomId}` }
}

/**
 * Branded room page — the core guest-facing surface (TR-3.1: SSR for branded
 * room pages). Baseline ships the shell only: brand header + access placeholder.
 * Per-room branding (logo, colours, metadata, About) is FR-1.1, a later wave.
 */
export default async function RoomPage({ params }: RoomPageProps) {
  const { roomId } = await params

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-12 sm:px-6">
      <section className="rounded-xl border bg-card p-6 shadow-sm">
        <div className="flex items-center gap-4">
          <div
            className="flex size-14 items-center justify-center rounded-lg bg-primary text-primary-foreground"
            aria-hidden
          >
            <ShieldCheck className="size-7" />
          </div>
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Invitation-only data room
            </p>
            <h1 className="text-2xl font-semibold tracking-tight">{roomId}</h1>
          </div>
        </div>
      </section>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <LockKeyhole className="size-4 text-primary" aria-hidden />
            Room access
          </CardTitle>
          <CardDescription>
            This room is protected. The NDA/consent gate and secure viewer are
            implemented in later waves.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Document access is enforced server-side and every page view is
          audit-logged (FR-1.x, TR-4.x).
        </CardContent>
      </Card>
    </div>
  )
}

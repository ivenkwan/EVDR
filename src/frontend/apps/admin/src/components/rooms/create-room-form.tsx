"use client"

import { useRouter } from "next/navigation"
import { useQueryClient } from "@tanstack/react-query"
import { useState } from "react"

import { Button } from "@evdr/ui"
import { Loader2 } from "lucide-react"

import { Field, inputClass, textareaClass } from "@/components/rooms/form-controls"
import { createRoom, roomKeys } from "@/lib/rooms-api"
import { ApiError } from "@/lib/api"

/**
 * Create-room form (FR-1.1 subset available today: name, slug, accent
 * colour theme override, About page text). Branding fields the Room Service
 * does not yet carry (logo asset pipeline) are intentionally absent.
 */
export function CreateRoomForm({ onDone }: { onDone?: () => void }) {
  const router = useRouter()
  const queryClient = useQueryClient()

  const [name, setName] = useState("")
  const [slug, setSlug] = useState("")
  const [accent, setAccent] = useState("#1e3a5f")
  const [aboutPage, setAboutPage] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function onSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      const theme: Record<string, string> = accent ? { "--primary": accent, "--ring": accent } : {}
      const room = await createRoom({
        name: name.trim(),
        slug: slug.trim(),
        branding: {
          ...(Object.keys(theme).length > 0 ? { theme } : {}),
          ...(aboutPage.trim() !== "" ? { aboutPage: aboutPage.trim() } : {}),
        },
      })
      await queryClient.invalidateQueries({ queryKey: roomKeys.all })
      onDone?.()
      router.push(`/rooms/${encodeURIComponent(room.id)}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Could not create the room.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Room name" htmlFor="room-name" hint="Shown to room members and guests.">
          <input
            id="room-name"
            className={inputClass}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Acme M&A data room"
            required
            maxLength={200}
          />
        </Field>
        <Field
          label="Slug"
          htmlFor="room-slug"
          hint="Portal URL segment — lowercase letters, digits, hyphens."
        >
          <input
            id="room-slug"
            className={inputClass}
            value={slug}
            onChange={(event) => setSlug(event.target.value)}
            placeholder="acme-ma"
            required
            pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
            maxLength={64}
          />
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Brand accent"
          htmlFor="room-accent"
          hint="Theme override applied to the guest room page (CSS variables)."
        >
          <input
            id="room-accent"
            type="color"
            className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-1"
            value={accent}
            onChange={(event) => setAccent(event.target.value)}
          />
        </Field>
        <Field
          label="About page"
          htmlFor="room-about"
          hint="Optional text guests see on the room page (markdown rendering later)."
        >
          <textarea
            id="room-about"
            className={textareaClass}
            value={aboutPage}
            onChange={(event) => setAboutPage(event.target.value)}
            rows={2}
            maxLength={4000}
          />
        </Field>
      </div>
      {error ? (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <div className="flex justify-end gap-2">
        <Button type="submit" disabled={submitting}>
          {submitting ? <Loader2 className="animate-spin" aria-hidden /> : null}
          {submitting ? "Creating…" : "Create room"}
        </Button>
      </div>
    </form>
  )
}

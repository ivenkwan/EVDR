"use client"

import { useQueryClient } from "@tanstack/react-query"
import { useRef, useState } from "react"

import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { FileUp, Loader2 } from "lucide-react"

import { Field, inputClass, selectClass } from "@/components/rooms/form-controls"
import { ApiError } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import {
  roomKeys,
  uploadDocument,
  validateUploadFile,
  ALLOWED_DOCUMENT_EXTENSIONS,
} from "@/lib/rooms-api"

/**
 * Document upload card (FR-2.1): client-side extension + size validation,
 * progress indication while the multipart POST runs through the BFF, and a
 * final "storing" phase while the Room Service persists the version. Uploads
 * of the same name + folder create the next immutable version (FR-2.2).
 */
export function UploadDocument({ roomId }: { roomId: string }) {
  const queryClient = useQueryClient()
  const fileInput = useRef<HTMLInputElement>(null)

  const [file, setFile] = useState<File | null>(null)
  const [folderPath, setFolderPath] = useState("")
  const [classification, setClassification] = useState("")
  const [progress, setProgress] = useState<number | null>(null)
  const [phase, setPhase] = useState<"idle" | "uploading" | "storing">("idle")
  const [error, setError] = useState<string | null>(null)
  const [lastUpload, setLastUpload] = useState<string | null>(null)

  function onPickFile(selected: File | null) {
    setError(null)
    setLastUpload(null)
    if (!selected) {
      setFile(null)
      return
    }
    const check = validateUploadFile(selected)
    if (!check.ok) {
      setFile(null)
      setError(check.error)
      if (fileInput.current) fileInput.current.value = ""
      return
    }
    setFile(selected)
  }

  async function onSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!file) return
    setError(null)
    setLastUpload(null)
    setPhase("uploading")
    setProgress(0)
    try {
      const document = await uploadDocument({
        roomId,
        file,
        folderPath,
        classification: classification === "" ? "" : classification,
        onProgress: (percent) => setProgress(percent),
      })
      setPhase("storing")
      await queryClient.invalidateQueries({ queryKey: roomKeys.documents(roomId) })
      setLastUpload(`Stored ${document.name} (${formatBytes(document.sizeBytes)}) as a new version.`)
      setFile(null)
      if (fileInput.current) fileInput.current.value = ""
      setFolderPath("")
      setClassification("")
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Upload failed.")
    } finally {
      setPhase("idle")
      setProgress(null)
    }
  }

  const busy = phase !== "idle"
  const progressLabel =
    phase === "uploading" && progress !== null
      ? `Uploading… ${progress}%`
      : phase === "storing"
        ? "Storing version in the room…"
        : null

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <FileUp className="size-4 text-primary" aria-hidden />
          Upload document
        </CardTitle>
        <CardDescription>
          PDF, DOCX, XLSX, PPTX and images, up to 100 MiB. Re-uploading the
          same name and folder adds an immutable version (FR-2.2).
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-4">
          <Field
            label="File"
            htmlFor="doc-file"
            hint={`Allowed: ${ALLOWED_DOCUMENT_EXTENSIONS.join(" ")}`}
          >
            <input
              id="doc-file"
              ref={fileInput}
              type="file"
              className="block w-full text-sm file:mr-3 file:rounded-md file:border-0 file:bg-secondary file:px-3 file:py-1.5 file:text-sm file:font-medium"
              onChange={(event) => onPickFile(event.target.files?.[0] ?? null)}
              disabled={busy}
              required
            />
          </Field>
          {file ? (
            <p className="text-xs text-muted-foreground">
              {file.name} · {formatBytes(file.size)}
            </p>
          ) : null}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field
              label="Folder (optional)"
              htmlFor="doc-folder"
              hint="Slash-separated path inside the room (FR-2.3)."
            >
              <input
                id="doc-folder"
                className={inputClass}
                value={folderPath}
                onChange={(event) => setFolderPath(event.target.value)}
                placeholder="finance/q3"
                disabled={busy}
              />
            </Field>
            <Field label="Classification" htmlFor="doc-class">
              <select
                id="doc-class"
                className={selectClass}
                value={classification}
                onChange={(event) => setClassification(event.target.value)}
                disabled={busy}
              >
                <option value="">Inherit room floor</option>
                <option value="PUBLIC">PUBLIC</option>
                <option value="INTERNAL">INTERNAL</option>
                <option value="CONFIDENTIAL">CONFIDENTIAL</option>
                <option value="RESTRICTED">RESTRICTED</option>
              </select>
            </Field>
          </div>

          {progressLabel ? (
            <div className="space-y-1">
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-primary transition-[width]"
                  style={{ width: `${progress ?? 0}%` }}
                  role="progressbar"
                  aria-valuenow={progress ?? 0}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label="Upload progress"
                />
              </div>
              <p className="text-xs text-muted-foreground">{progressLabel}</p>
            </div>
          ) : null}

          {error ? (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          ) : null}
          {lastUpload ? (
            <p className="text-sm text-emerald-700" role="status">
              {lastUpload}
            </p>
          ) : null}

          <div className="flex justify-end">
            <Button type="submit" disabled={busy || !file}>
              {busy ? <Loader2 className="animate-spin" aria-hidden /> : null}
              {busy ? "Uploading…" : "Upload document"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

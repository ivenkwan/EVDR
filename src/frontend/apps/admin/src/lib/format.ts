/**
 * Small display formatters shared by admin console components.
 */

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—"
  if (bytes < 1024) return `${bytes} B`
  const units = ["KiB", "MiB", "GiB", "TiB"]
  let value = bytes
  let unit = "B"
  for (const next of units) {
    value /= 1024
    unit = next
    if (value < 1024) break
  }
  return `${value.toFixed(value >= 100 ? 0 : 1)} ${unit}`
}

export function formatDateTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return new Intl.DateTimeFormat("en-GB", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date)
}

export function shortDigest(sha256: string): string {
  if (sha256.length <= 16) return sha256
  return `${sha256.slice(0, 8)}…${sha256.slice(-8)}`
}

/** Title-cases machine labels for display, e.g. "view_only" → "View only". */
export function humanize(label: string): string {
  return label
    .split(/[_-]+/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ")
}

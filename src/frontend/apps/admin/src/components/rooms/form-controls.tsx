import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from "react"

import { cn } from "@evdr/ui"

/**
 * Minimal labelled form controls for the admin console. The UI kit ships
 * Button + Card only (packages/ui is a shared baseline that other waves may
 * extend); interactive forms compose plain elements with the kit's tokens.
 */

export const inputClass =
  "flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"

export const textareaClass =
  "flex min-h-16 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"

export const selectClass = cn(inputClass, "pr-8")

export function Field({
  label,
  htmlFor,
  hint,
  error,
  children,
}: {
  label: string
  htmlFor?: string
  hint?: string
  error?: string
  children: ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="text-sm font-medium leading-none">
        {label}
      </label>
      {children}
      {error ? (
        <p className="text-xs text-destructive" role="alert">
          {error}
        </p>
      ) : hint ? (
        <p className="text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  )
}

export type TextInputProps = InputHTMLAttributes<HTMLInputElement>
export type SelectInputProps = SelectHTMLAttributes<HTMLSelectElement>
export type AreaInputProps = TextareaHTMLAttributes<HTMLTextAreaElement>

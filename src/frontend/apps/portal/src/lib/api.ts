/**
 * API gateway client — the only way app code talks to the backend.
 * AGENTS.md: "All data fetching via TanStack Query against API gateway —
 * never fetch() directly in components."
 *
 * Baseline wires the transport only. Real endpoints get Zod-validated
 * response schemas once API contracts land (later waves).
 */

export const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL ?? "/api/v1"

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, message: string, code = "api_error") {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

export interface ApiRequestOptions extends RequestInit {
  /** Expected response type; defaults to JSON. */
  responseType?: "json"
}

async function parseError(response: Response): Promise<ApiError> {
  let message = `Request failed with status ${response.status}`
  let code = "api_error"
  try {
    const body = (await response.json()) as { message?: string; code?: string }
    if (body.message) message = body.message
    if (body.code) code = body.code
  } catch {
    // Non-JSON error body — keep defaults.
  }
  return new ApiError(response.status, message, code)
}

export async function apiFetch<T>(path: string, init: ApiRequestOptions = {}): Promise<T> {
  const { responseType = "json", ...requestInit } = init

  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...requestInit,
    headers: {
      "content-type": "application/json",
      ...requestInit.headers,
    },
  })

  if (!response.ok) {
    throw await parseError(response)
  }

  if (response.status === 204 || responseType === "json") {
    return undefined as T
  }
  return (await response.json()) as T
}

export const api = {
  get: <T>(path: string, init?: ApiRequestOptions) =>
    apiFetch<T>(path, { ...init, method: "GET" }),
  post: <T>(path: string, body?: unknown, init?: ApiRequestOptions) =>
    apiFetch<T>(path, {
      ...init,
      method: "POST",
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
}

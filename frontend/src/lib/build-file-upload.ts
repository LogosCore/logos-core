import { ApiError, tryRefresh } from "@/services/api-client"

/** A build file as POST /build-files returns it. */
export interface UploadedBuildFile {
  id: string
  name: string
  size: number
  sha256: string
  mediaType?: string
}

export interface UploadBuildFileOptions {
  /** Progress callback; pct is 0..1. */
  onProgress?: (pct: number) => void
  signal?: AbortSignal
}

const API_URL = import.meta.env.VITE_API_URL as string

/** The URL a build's artifact downloads from (the session cookie authenticates it). */
export function artifactUrl(buildId: string): string {
  return `${API_URL}/builds/${encodeURIComponent(buildId)}/artifact`
}

function readCsrfToken(): string | null {
  if (typeof document === "undefined") return null
  const match = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]*)/)
  return match ? decodeURIComponent(match[1]) : null
}

/**
 * POST a file for a build's file fields to the operation, with upload
 * progress. On 401 a single refresh-and-retry is attempted, like apiFetch
 * (XHR cannot go through it). The server enforces its own size limit.
 */
export async function uploadBuildFile(
  file: File,
  operationId: string,
  opts: UploadBuildFileOptions = {},
): Promise<UploadedBuildFile> {
  const send = () => postFile(file, operationId, opts)
  try {
    return await send()
  } catch (err) {
    if (err instanceof ApiError && err.status === 401 && (await tryRefresh())) return send()
    throw err
  }
}

function postFile(file: File, operationId: string, opts: UploadBuildFileOptions): Promise<UploadedBuildFile> {
  return new Promise((resolve, reject) => {
    const form = new FormData()
    form.append("operationId", operationId)
    form.append("file", file, file.name || "upload")

    const xhr = new XMLHttpRequest()
    xhr.open("POST", `${API_URL}/build-files`, true)
    xhr.withCredentials = true
    const csrf = readCsrfToken()
    if (csrf) xhr.setRequestHeader("X-CSRF-Token", csrf)

    if (opts.onProgress) {
      xhr.upload.addEventListener("progress", (ev) => {
        if (ev.lengthComputable && ev.total > 0) opts.onProgress?.(ev.loaded / ev.total)
      })
    }
    const abort = () => xhr.abort()
    if (opts.signal) {
      if (opts.signal.aborted) {
        reject(new DOMException("Aborted", "AbortError"))
        return
      }
      opts.signal.addEventListener("abort", abort, { once: true })
    }

    xhr.addEventListener("load", () => {
      opts.signal?.removeEventListener("abort", abort)
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as UploadedBuildFile)
        } catch (parseErr) {
          reject(parseErr)
        }
        return
      }
      reject(new ApiError(xhr.status, errorText(xhr.responseText) ?? "upload failed"))
    })
    xhr.addEventListener("error", () => {
      opts.signal?.removeEventListener("abort", abort)
      reject(new ApiError(0, "Network error"))
    })
    xhr.addEventListener("abort", () => {
      opts.signal?.removeEventListener("abort", abort)
      reject(new DOMException("Aborted", "AbortError"))
    })
    xhr.send(form)
  })
}

function errorText(body: string): string | null {
  if (!body) return null
  try {
    return (JSON.parse(body) as { error?: string }).error ?? null
  } catch {
    return null
  }
}

import { ApiError, getCsrfToken, tryRefresh } from "@/services/api-client"

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
  return withRefresh(() => postFile<UploadedBuildFile>("/build-files", file, operationId, opts))
}

/** A task file as POST /minion-files returns it: one to send a minion with a task. */
export interface UploadedTaskFile {
  id: string
  name: string
  size: number
  sha256: string
  contentType: string
}

/**
 * POST a file for a command's file param to the operation (docs:
 * contracts/task-results-and-actions.md, Task files), with upload progress.
 * Core streams it into its minion files bucket and keeps it while a task
 * that names it has not ended.
 */
export async function uploadTaskFile(
  file: File,
  operationId: string,
  opts: UploadBuildFileOptions = {},
): Promise<UploadedTaskFile> {
  return withRefresh(() => postFile<UploadedTaskFile>("/minion-files", file, operationId, opts))
}

async function withRefresh<T>(send: () => Promise<T>): Promise<T> {
  try {
    return await send()
  } catch (err) {
    if (err instanceof ApiError && err.status === 401 && (await tryRefresh())) return send()
    throw err
  }
}

function postFile<T>(path: string, file: File, operationId: string, opts: UploadBuildFileOptions): Promise<T> {
  return new Promise((resolve, reject) => {
    // operationId first: core reads the body as it streams, and checks the
    // caller's role before it takes the file.
    const form = new FormData()
    form.append("operationId", operationId)
    form.append("file", file, file.name || "upload")

    const xhr = new XMLHttpRequest()
    xhr.open("POST", `${API_URL}${path}`, true)
    xhr.withCredentials = true
    const csrf = getCsrfToken()
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
          resolve(JSON.parse(xhr.responseText) as T)
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

import { downloadBlob } from "@/lib/file-export"
import { apiFetch } from "@/services/api-client"
import { parseCast, type Cast } from "./cast-values"
import { recordingFileName, type Recording } from "./recording-values"
import { castTranscriptCode } from "./terminal-recording-code"

// A terminal's recording read from core (GET /api/v1/recordings/{id}): for
// its player, and for its transcript, made in the browser.

/** A recording's asciicast, read whole (one still recording: as far as core kept it). */
export async function fetchCast(path: string, signal?: AbortSignal): Promise<Cast> {
  const res = await apiFetch(path, { timeoutMs: 0, signal })
  if (!res.ok) throw new Error(await refusal(res))
  return parseCast(await res.text())
}

/** What core said, refusing it: its error's message, else the status. */
async function refusal(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: unknown; message?: unknown }
    const said = body.error ?? body.message
    if (typeof said === "string" && said !== "") return said
  } catch {
    // Not core's JSON.
  }
  return `core answered ${res.status}`
}

/** Saves a terminal recording's transcript (cast-transcript.ts), as terminal-<task>-<n>.txt. */
export async function downloadTranscript(r: Recording, cast?: Cast): Promise<void> {
  const [read, code] = await Promise.all([cast ?? fetchCast(r.path), castTranscriptCode.load()])
  downloadBlob(await code.castTranscript(read), recordingFileName(r, "txt"), "text/plain")
}

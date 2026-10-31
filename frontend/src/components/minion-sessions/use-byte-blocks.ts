import { useCallback, useEffect, useRef, useState } from "react"
import { BLOCK_SIZE } from "./hex"

// A file's bytes for the hex view, read from the server a block at a time
// with byte ranges as the view needs them, the last MAX_BLOCKS kept: a file
// of any size costs a few MiB at most.

const MAX_BLOCKS = 64

/** Reads bytes start..end (end excluded) of the file at url. */
export async function readRange(url: string, start: number, end: number): Promise<Uint8Array> {
  const res = await fetch(url, { credentials: "same-origin", headers: { Range: `bytes=${start}-${end - 1}` } })
  if (!res.ok) throw new Error(`Couldn't read the file (HTTP ${res.status}).`)
  const body = new Uint8Array(await res.arrayBuffer())
  // A server that ignores the range sends it all.
  return res.status === 206 ? body : body.subarray(start, end)
}

export interface ByteBlocks {
  /** The byte at an offset, undefined until its block is read. */
  byteAt: (offset: number) => number | undefined
  /** Reads the blocks not read yet; the oldest go past MAX_BLOCKS. */
  want: (blocks: readonly number[]) => void
  /** Why a read failed, the last time one did. */
  error: string | null
}

export function useByteBlocks(url: string, size: number): ByteBlocks {
  const [blocks, setBlocks] = useState<ReadonlyMap<number, Uint8Array>>(() => new Map())
  const [error, setError] = useState<string | null>(null)
  // Blocks asked for and not read yet: asked once.
  const pending = useRef(new Set<number>())
  // Another file's reads land nowhere.
  const current = useRef(url)
  useEffect(() => {
    current.current = url
    pending.current = new Set()
    return () => {
      current.current = ""
    }
  }, [url])

  const want = useCallback(
    (wanted: readonly number[]) => {
      for (const i of wanted) {
        if (blocks.has(i) || pending.current.has(i)) continue
        pending.current.add(i)
        const start = i * BLOCK_SIZE
        readRange(url, start, Math.min(start + BLOCK_SIZE, size)).then(
          (data) => {
            pending.current.delete(i)
            if (current.current !== url) return
            setBlocks((prev) => {
              const next = new Map(prev)
              next.delete(i)
              next.set(i, data)
              // Map order is insertion order: the first are the oldest.
              for (const k of next.keys()) {
                if (next.size <= MAX_BLOCKS) break
                if (!wanted.includes(k)) next.delete(k)
              }
              return next
            })
          },
          (err: unknown) => {
            pending.current.delete(i)
            if (current.current === url) setError(err instanceof Error ? err.message : "Couldn't read the file.")
          },
        )
      }
    },
    [blocks, url, size],
  )

  const byteAt = useCallback(
    (offset: number) => blocks.get(Math.floor(offset / BLOCK_SIZE))?.[offset % BLOCK_SIZE],
    [blocks],
  )

  return { byteAt, want, error }
}

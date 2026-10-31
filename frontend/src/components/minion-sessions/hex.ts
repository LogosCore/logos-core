// The hex view's arithmetic: rows of 16 bytes, read from the server in
// blocks, offsets as typed and shown, and what the bytes at the caret read
// as.

export const BYTES_PER_ROW = 16
/** How much of the file one read brings: 4096 rows. */
export const BLOCK_SIZE = 64 << 10

/** The offset column's width in hex digits: 8, or what the file needs. */
export function offsetDigits(size: number): number {
  return Math.max(8, Math.max(0, size - 1).toString(16).length)
}

export function formatOffset(offset: number, digits: number): string {
  return offset.toString(16).toUpperCase().padStart(digits, "0")
}

/** "0x1f40", "1f40h" or "$1f40" is hex; digits alone are decimal, unless
 *  they hold a-f. Null for anything else, or a negative. */
export function parseOffset(text: string): number | null {
  const t = text.trim().toLowerCase().replaceAll("_", "")
  if (t === "") return null
  let m = /^(?:0x|\$)([0-9a-f]+)$/.exec(t) ?? /^([0-9a-f]+)h$/.exec(t)
  if (m) return parseInt(m[1], 16)
  if (/^[0-9]+$/.test(t)) return parseInt(t, 10)
  m = /^([0-9a-f]+)$/.exec(t)
  return m ? parseInt(m[1], 16) : null
}

export function hexByte(b: number): string {
  return b.toString(16).toUpperCase().padStart(2, "0")
}

/** A byte in the text column: printable ASCII as itself, the rest a dot. */
export function asciiChar(b: number): string {
  return b >= 0x20 && b <= 0x7e ? String.fromCharCode(b) : "."
}

/** The blocks rows from..to (end excluded) are in. */
export function blocksFor(fromRow: number, toRow: number, size: number): number[] {
  if (size <= 0 || toRow <= fromRow) return []
  const first = Math.floor((fromRow * BYTES_PER_ROW) / BLOCK_SIZE)
  const last = Math.floor((Math.min(toRow * BYTES_PER_ROW, size) - 1) / BLOCK_SIZE)
  const out: number[] = []
  for (let i = first; i <= last; i++) out.push(i)
  return out
}

export interface Inspected {
  label: string
  value: string
}

/** What the bytes from the caret read as, little-endian, for as many as
 *  there are. */
export function inspect(bytes: readonly number[]): Inspected[] {
  const b = Uint8Array.from(bytes)
  const v = new DataView(b.buffer)
  const out: Inspected[] = []
  if (b.length >= 1) {
    out.push({ label: "u8", value: String(v.getUint8(0)) }, { label: "i8", value: String(v.getInt8(0)) })
  }
  if (b.length >= 2) {
    out.push({ label: "u16", value: String(v.getUint16(0, true)) }, { label: "i16", value: String(v.getInt16(0, true)) })
  }
  if (b.length >= 4) {
    out.push({ label: "u32", value: String(v.getUint32(0, true)) }, { label: "i32", value: String(v.getInt32(0, true)) })
  }
  if (b.length >= 8) {
    out.push({ label: "u64", value: v.getBigUint64(0, true).toString() })
  }
  return out
}

/** Bytes as hex pairs, space between. */
export function hexString(bytes: Uint8Array): string {
  return Array.from(bytes, hexByte).join(" ")
}

/** Bytes as text: printable ASCII as itself, tab and line ends kept, the
 *  rest a dot. */
export function asciiString(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => (b === 0x09 || b === 0x0a || b === 0x0d ? String.fromCharCode(b) : asciiChar(b))).join("")
}

/** A selection: from anchor to caret, either way round. */
export interface HexSelection {
  anchor: number
  caret: number
}

export interface HexPlace {
  /** The first row in sight, as asked: clamped to the file's end when drawn. */
  top: number
  sel: HexSelection | null
}

/** The place that shows length bytes from offset, selected, a few rows down. */
export function hexPlaceOf(offset: number, length: number): HexPlace {
  return {
    top: Math.max(0, Math.floor(offset / BYTES_PER_ROW) - 4),
    sel: { anchor: offset, caret: offset + Math.max(1, length) - 1 },
  }
}

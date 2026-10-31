import { describe, expect, it } from "vitest"
import { BLOCK_SIZE, asciiString, blocksFor, formatOffset, hexString, inspect, offsetDigits, parseOffset } from "./hex"

describe("parseOffset", () => {
  it("reads hex and decimal", () => {
    expect(parseOffset("0x1F40")).toBe(0x1f40)
    expect(parseOffset("$ff")).toBe(255)
    expect(parseOffset("1f40h")).toBe(0x1f40)
    expect(parseOffset("8000")).toBe(8000)
    expect(parseOffset("dead")).toBe(0xdead)
    expect(parseOffset(" 1_000 ")).toBe(1000)
  })
  it("refuses the rest", () => {
    expect(parseOffset("")).toBeNull()
    expect(parseOffset("-1")).toBeNull()
    expect(parseOffset("0xzz")).toBeNull()
  })
})

describe("offsets", () => {
  it("pads to 8 digits, more for a file past 4 GiB", () => {
    expect(offsetDigits(100)).toBe(8)
    expect(offsetDigits(2 ** 32 + 1)).toBe(9)
    expect(formatOffset(0x1f40, 8)).toBe("00001F40")
  })
})

describe("blocksFor", () => {
  it("covers the rows, within the file", () => {
    expect(blocksFor(0, 10, 100)).toEqual([0])
    expect(blocksFor(4090, 4100, 10 * BLOCK_SIZE)).toEqual([0, 1])
    expect(blocksFor(0, 10, 0)).toEqual([])
  })
})

describe("inspect", () => {
  it("reads little-endian", () => {
    const got = Object.fromEntries(inspect([0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0]).map((i) => [i.label, i.value]))
    expect(got).toMatchObject({ u8: "255", i8: "-1", u16: "65535", i16: "-1", u32: "4294967295", i32: "-1", u64: "8589934591" })
    expect(inspect([7]).map((i) => i.label)).toEqual(["u8", "i8"])
  })
})

describe("copying", () => {
  it("as hex and as text", () => {
    const b = Uint8Array.from([0x41, 0x0a, 0x00, 0xff])
    expect(hexString(b)).toBe("41 0A 00 FF")
    expect(asciiString(b)).toBe("A\n..")
  })
})

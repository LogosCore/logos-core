import { describe, expect, it } from "vitest"
import { readVolumes, usedShare, volumeKind } from "./volumes"

describe("readVolumes", () => {
  it("reads a host's volumes, a kind it does not know as other", () => {
    const v = readVolumes({
      sep: "\\",
      volumes: [
        { path: "C:\\", kind: "fixed", label: "System", fs: "NTFS", size: 100, free: 25 },
        { path: "D:\\", kind: "optical", no_media: true },
        { path: "Q:\\", kind: "holographic", read_only: true },
      ],
    })
    expect(v?.sep).toBe("\\")
    expect(v?.volumes.map((x) => [x.path, x.kind, x.noMedia, x.readOnly])).toEqual([
      ["C:\\", "fixed", false, false],
      ["D:\\", "optical", true, false],
      ["Q:\\", "other", false, true],
    ])
    expect(v?.volumes[0]).toMatchObject({ label: "System", fs: "NTFS", size: 100, free: 25 })
  })

  it("is null for data that is not volumes", () => {
    expect(readVolumes(null)).toBeNull()
    expect(readVolumes({ volumes: [] })).toBeNull()
    expect(readVolumes({ sep: "/", volumes: [{ kind: "fixed" }] })).toBeNull()
    expect(readVolumes({ sep: "/" })).toEqual({ sep: "/", volumes: [] })
  })
})

describe("usedShare", () => {
  it("is how full it is, when its size and free are known", () => {
    expect(usedShare({ size: 100, free: 25 })).toBe(0.75)
    expect(usedShare({ size: 100 })).toBeNull()
    expect(usedShare({ size: 0, free: 0 })).toBeNull()
  })
})

describe("volumeKind", () => {
  it("takes the kinds it knows", () => {
    expect(volumeKind("network")).toBe("network")
    expect(volumeKind(3)).toBe("other")
  })
})

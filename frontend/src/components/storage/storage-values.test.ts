import { describe, expect, it } from "vitest"
import {
  actionResult,
  actionTitle,
  bucketAbout,
  bucketName,
  bucketWarnings,
  defaultBucket,
  deletedShare,
  kindLabel,
  purposeLabel,
  storeBytes,
  volumesText,
  warningMark,
  worstLevel,
  type StorageWarning,
} from "./storage-values"

const warning = (level: string, bucket: string | null = null): StorageWarning => ({ level, code: "c", bucket, message: "m" })

describe("buckets", () => {
  it("says what a bucket holds and whose it is", () => {
    expect(bucketAbout({ name: "minion-files", purposes: ["minion_files"] })).toBe("Minion files")
    expect(bucketAbout({ name: "shared", purposes: ["wiki_images", "wiki_files"] })).toBe("Wiki images, Wiki files")
    expect(bucketAbout({ name: "itest-blob", purposes: [] })).toBe("not core's")
    expect(bucketAbout({ name: "", purposes: [] })).toBe("written outside any bucket")
    expect(bucketName("")).toBe("SeaweedFS's own files")
    expect(bucketName("skills")).toBe("skills")
    expect(purposeLabel("recordings")).toBe("Recordings")
    expect(purposeLabel("later")).toBe("later")
  })
  it("says its volumes", () => {
    expect(volumesText({ volumes: 0, writableVolumes: 0, readOnlyVolumes: 0 })).toMatch(/^none yet/)
    expect(volumesText({ volumes: 7, writableVolumes: 7, readOnlyVolumes: 0 })).toBe("7 (7 writable)")
    expect(volumesText({ volumes: 3, writableVolumes: 0, readOnlyVolumes: 3 })).toBe("3 (0 writable, 3 read-only)")
  })
  it("opens the breakdown on core's bucket that holds the most", () => {
    expect(
      defaultBucket([
        { name: "", purposes: [], objectBytes: 0 },
        { name: "wiki-files", purposes: ["wiki_files"], objectBytes: 10 },
        { name: "minion-files", purposes: ["minion_files"], objectBytes: 90 },
        { name: "itest-blob", purposes: [], objectBytes: 1000 },
      ]),
    ).toBe("minion-files")
    expect(defaultBucket([{ name: "itest-blob", purposes: [], objectBytes: 5 }])).toBeNull()
  })
  it("measures deleted files' share and the store", () => {
    expect(deletedShare({ size: 200, deletedBytes: 50 })).toBe(0.25)
    expect(deletedShare({ size: 0, deletedBytes: 0 })).toBe(0)
    expect(storeBytes([{ size: 1 }, { size: 2 }])).toBe(3)
  })
})

describe("kinds", () => {
  it("names each kind of record", () => {
    expect(kindLabel("minion_files", "upload")).toBe("Task uploads")
    expect(kindLabel("minion_files", "download")).toBe("Downloads")
    expect(kindLabel("recordings", "terminal")).toBe("Terminals")
    expect(kindLabel("builds", "artifact")).toBe("Artifacts")
    expect(kindLabel("skills", "version")).toBe("Skill versions")
    expect(kindLabel("builds", "later")).toBe("later")
  })
})

describe("actions", () => {
  const a = (over: Partial<Parameters<typeof actionResult>[0]>) => ({ action: "reclaim", status: "succeeded", error: null, freed: 0, volumes: 0, ...over })
  it("says what an action did", () => {
    expect(actionResult(a({ freed: 3 * 1024 * 1024 }))).toBe("freed 3.0 MB")
    expect(actionResult(a({ action: "grow", volumes: 1 }))).toBe("added 1 volume")
    expect(actionResult(a({ action: "grow", volumes: 2 }))).toBe("added 2 volumes")
    expect(actionResult(a({ status: "running" }))).toMatch(/^running: SeaweedFS compacts/)
    expect(actionResult(a({ action: "grow", status: "failed", error: "SeaweedFS: only 0 volumes left" }))).toBe("SeaweedFS: only 0 volumes left")
  })
  it("names it", () => {
    expect(actionTitle({ action: "reclaim", bucket: null })).toBe("Reclaim space")
    expect(actionTitle({ action: "grow", bucket: "skills" })).toBe("Add a volume to skills")
  })
})

describe("warnings", () => {
  it("marks the sidebar with those that need an admin, the worst's colour", () => {
    expect(warningMark([])).toBeNull()
    expect(warningMark([warning("info")])).toBeNull()
    expect(warningMark([warning("warning"), warning("info")])).toEqual({ count: 1, level: "warning" })
    expect(warningMark([warning("warning"), warning("error")])).toEqual({ count: 2, level: "error" })
  })
  it("finds the worst, and a bucket's own", () => {
    expect(worstLevel([warning("info"), warning("warning")])).toBe("warning")
    expect(worstLevel([])).toBeNull()
    const ws = [warning("info", "minion-files"), warning("warning", null), warning("info", "")]
    expect(bucketWarnings(ws, "minion-files")).toHaveLength(1)
    expect(bucketWarnings(ws, "")).toHaveLength(1)
  })
})

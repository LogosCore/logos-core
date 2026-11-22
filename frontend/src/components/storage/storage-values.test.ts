import { describe, expect, it } from "vitest"
import {
  actionResult,
  actionTitle,
  bucketAbout,
  bucketName,
  bucketWarnings,
  cancellable,
  daysText,
  defaultBucket,
  deletedShare,
  kindLabel,
  nextRunText,
  parseDays,
  purposeLabel,
  recordingsText,
  reclaimCounts,
  storeBytes,
  volumeStateLabel,
  volumesText,
  worstLevel,
  type StorageWarning,
} from "./storage-values"
import { warningMark } from "./warning-mark"

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
  const a = (over: Partial<Parameters<typeof actionResult>[0]>): Parameters<typeof actionResult>[0] => ({
    action: "reclaim",
    status: "succeeded",
    error: null,
    freed: 0,
    volumes: 0,
    recordings: 0,
    bytes: 0,
    plannedRecordings: 0,
    step: null,
    cancelRequestedAt: null,
    cancelledBy: null,
    reclaimVolumes: [],
    ...over,
  })
  const vols = (...states: string[]) => states.map((state) => ({ state }))
  const bob = { type: "user", id: "u2", name: "bob" }
  it("says what a grow did", () => {
    expect(actionResult(a({ action: "grow", volumes: 1 }))).toBe("added 1 volume")
    expect(actionResult(a({ action: "grow", volumes: 2 }))).toBe("added 2 volumes")
    expect(actionResult(a({ action: "grow", status: "failed", error: "SeaweedFS: only 0 volumes left" }))).toBe("SeaweedFS: only 0 volumes left")
  })
  it("says where a reclaim is, and what it freed from how many volumes", () => {
    const mb = 1024 * 1024
    expect(actionResult(a({ status: "running", step: "reading the volumes" }))).toBe("reading the volumes")
    expect(
      actionResult(
        a({
          status: "running",
          step: "compacting volume 39 of minion-files (2 of 3)",
          freed: 3 * mb,
          reclaimVolumes: vols("compacted", "compacting", "waiting"),
        }),
      ),
    ).toBe("compacting volume 39 of minion-files (2 of 3): 1 of 3 volumes done, freed 3.0 MB so far")
    expect(
      actionResult(a({ status: "running", step: "compacting volume 39", cancelRequestedAt: "t", reclaimVolumes: vols("compacting") })),
    ).toBe("stopping after this volume (compacting volume 39): 0 of 1 volume done, freed 0 B so far")
    expect(actionResult(a({ freed: 3 * mb, reclaimVolumes: vols("compacted", "compacted", "skipped") }))).toBe(
      "freed 3.0 MB from 2 volumes, 1 skipped",
    )
    expect(actionResult(a({ freed: mb, reclaimVolumes: vols("compacted") }))).toBe("freed 1.0 MB from 1 volume")
    expect(actionResult(a({}))).toBe("nothing to compact: deleted files held a tenth of no volume")
    expect(
      actionResult(a({ status: "failed", error: "1 of 2 volumes were not compacted: each says why", freed: mb, reclaimVolumes: vols("compacted", "failed") })),
    ).toBe("1 of 2 volumes were not compacted: each says why (freed 1.0 MB before)")
    expect(actionResult(a({ status: "failed", error: "core stopped before it ended" }))).toBe("core stopped before it ended")
    expect(
      actionResult(a({ status: "cancelled", cancelledBy: bob, freed: mb, reclaimVolumes: vols("compacted", "stopped", "stopped") })),
    ).toBe("cancelled by bob: freed 1.0 MB from 1 of 3 volumes")
  })
  it("counts a reclaim's volumes, and says which actions stop", () => {
    expect(reclaimCounts(vols("compacted", "skipped", "failed", "stopped", "waiting", "compacting"))).toEqual({
      total: 6,
      compacted: 1,
      skipped: 1,
      failed: 1,
      stopped: 1,
      done: 3,
    })
    expect(cancellable({ action: "reclaim", status: "running", cancelRequestedAt: null })).toBe(true)
    expect(cancellable({ action: "cleanup", status: "running", cancelRequestedAt: null })).toBe(true)
    expect(cancellable({ action: "reclaim", status: "running", cancelRequestedAt: "t" })).toBe(false)
    expect(cancellable({ action: "reclaim", status: "succeeded", cancelRequestedAt: null })).toBe(false)
    expect(cancellable({ action: "grow", status: "running", cancelRequestedAt: null })).toBe(false)
    expect(volumeStateLabel("compacted")).toBe("compacted")
    expect(volumeStateLabel("later")).toBe("later")
  })
  it("says what a cleanup took: so far while it runs, before it failed", () => {
    const c = (over: Partial<Parameters<typeof actionResult>[0]>) => a({ action: "cleanup", ...over })
    expect(actionResult(c({ recordings: 12, bytes: 3 * 1024 * 1024 }))).toBe("took 12 recordings, 3.0 MB")
    expect(actionResult(c({ recordings: 1, bytes: 1024 }))).toBe("took 1 recording, 1.0 KB")
    expect(actionResult(c({}))).toBe("took none")
    expect(actionResult(c({ status: "running" }))).toBe("running: none taken yet")
    expect(actionResult(c({ status: "running", recordings: 5, bytes: 1024 }))).toBe("running: took 5 recordings, 1.0 KB so far")
    expect(actionResult(c({ status: "failed", error: "core stopped before it ended", recordings: 2, bytes: 1024 }))).toBe(
      "core stopped before it ended (it took 2 recordings, 1.0 KB before)",
    )
    expect(actionResult(c({ status: "failed", error: "store down" }))).toBe("store down")
    expect(actionResult(c({ status: "running", plannedRecordings: 12, recordings: 5, bytes: 1024 }))).toBe(
      "running: took 5 of about 12 recordings, 1.0 KB so far",
    )
    expect(actionResult(c({ status: "running", plannedRecordings: 12, cancelRequestedAt: "t" }))).toBe(
      "stopping after this recording: took 0 of about 12 recordings, 0 B so far",
    )
    expect(actionResult(c({ status: "cancelled", cancelledBy: bob, recordings: 1, bytes: 10 }))).toBe("cancelled by bob: took 1 recording, 10 B")
    expect(actionResult(c({ status: "cancelled" }))).toBe("cancelled: took none")
  })
  it("names it", () => {
    const t = { olderThanDays: null, operationId: null, operationName: null }
    expect(actionTitle({ action: "reclaim", bucket: null, ...t })).toBe("Reclaim space")
    expect(actionTitle({ action: "grow", bucket: "skills", ...t })).toBe("Add a volume to skills")
    expect(actionTitle({ action: "cleanup", bucket: "desktop-recordings", ...t, olderThanDays: 30 })).toBe(
      "Clean up recordings (30 days, every operation)",
    )
    expect(actionTitle({ action: "cleanup", bucket: "r", olderThanDays: 1, operationId: "o", operationName: "Red" })).toBe(
      "Clean up recordings (1 day, Red)",
    )
    expect(actionTitle({ action: "cleanup", bucket: "r", olderThanDays: 7, operationId: "o", operationName: null })).toBe(
      "Clean up recordings (7 days, an operation gone)",
    )
  })
})

describe("retention", () => {
  it("reads an age in days: a whole number, 1 to 3650", () => {
    expect(parseDays("30")).toBe(30)
    expect(parseDays(" 7 ")).toBe(7)
    expect(parseDays("3650")).toBe(3650)
    for (const bad of ["", "0", "3651", "-1", "1.5", "1e3", "thirty", "30 days"]) expect(parseDays(bad)).toBeNull()
  })
  it("says ages, counts and the next run", () => {
    expect(daysText(1)).toBe("1 day")
    expect(daysText(30)).toBe("30 days")
    expect(recordingsText(1)).toBe("1 recording")
    expect(recordingsText(1200)).toBe("1,200 recordings")
    const now = Date.parse("2026-11-22T10:00:00Z")
    expect(nextRunText(null, now)).toBeNull()
    expect(nextRunText("2026-11-22T09:00:00Z", now)).toBe("due: it starts within minutes")
    expect(nextRunText("2026-11-23T10:00:00Z", now)).toMatch(/^2026\.11\.23 \d\d:00:00$/)
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

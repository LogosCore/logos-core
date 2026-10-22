import { describe, expect, it } from "vitest"

import { detectMediaPreviewKind } from "./wiki-file-preview-media"

describe("detectMediaPreviewKind", () => {
  it("recognises browser-playable containers by content type", () => {
    expect(detectMediaPreviewKind("video/webm", "capture.webm")).toBe("video")
    expect(detectMediaPreviewKind("video/mp4", "demo.mp4")).toBe("video")
    expect(detectMediaPreviewKind("audio/mpeg", "call.mp3")).toBe("audio")
    expect(detectMediaPreviewKind("audio/x-wav", "call.wav")).toBe("audio")
  })

  it("falls back to the extension for a generic type", () => {
    expect(
      detectMediaPreviewKind("application/octet-stream", "capture.webm"),
    ).toBe("video")
    expect(detectMediaPreviewKind("application/octet-stream", "note.m4a")).toBe(
      "audio",
    )
  })

  it("gives QuickTime a player, which reports a codec it cannot decode", () => {
    expect(detectMediaPreviewKind("video/quicktime", "clip.mov")).toBe("video")
    expect(detectMediaPreviewKind("application/octet-stream", "IMG_0042.MOV")).toBe("video")
  })

  it("offers no player for media browsers do not decode, or other files", () => {
    expect(detectMediaPreviewKind("video/x-msvideo", "clip.avi")).toBeNull()
    expect(detectMediaPreviewKind("application/pdf", "report.pdf")).toBeNull()
    expect(detectMediaPreviewKind("text/plain", "notes.txt")).toBeNull()
  })
})

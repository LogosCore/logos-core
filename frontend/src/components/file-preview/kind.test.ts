import { describe, expect, it } from "vitest"
import { filePreviewKind, liveKind } from "./kind"

describe("liveKind", () => {
  const of = (contentType: string, name: string) => liveKind(filePreviewKind(contentType, name, 1000), contentType)

  it("draws pictures the browser draws as their bytes come", () => {
    expect(of("image/png", "screenshot-1.png")).toBe("image")
    expect(of("image/jpeg", "screenshot-1.jpg")).toBe("image")
  })

  it("grows text", () => {
    expect(of("text/plain", "boot.log")).toBe("text")
    expect(of("application/json", "config.json")).toBe("text")
  })

  it("waits for the whole of the rest", () => {
    expect(of("application/pdf", "report.pdf")).toBeNull()
    expect(of("video/mp4", "clip.mp4")).toBeNull()
    expect(of("image/svg+xml", "logo.svg")).toBeNull()
    expect(of("application/zip", "loot.zip")).toBeNull()
  })
})

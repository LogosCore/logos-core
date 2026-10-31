import { describe, expect, it } from "vitest"
import { fileTypeName } from "./type-name"

describe("fileTypeName", () => {
  it("names known types", () => {
    expect(fileTypeName("application/vnd.openxmlformats-officedocument.wordprocessingml.document", "a.docx")).toBe("Word document")
    expect(fileTypeName("application/x-executable", "bin")).toBe("ELF executable")
    expect(fileTypeName("text/plain; charset=utf-8", "notes")).toBe("Text")
  })
  it("names media by subtype", () => {
    expect(fileTypeName("image/png", "x.png")).toBe("PNG image")
    expect(fileTypeName("video/x-matroska", "x.mkv")).toBe("MATROSKA video")
  })
  it("lets the name speak over a generic sniff", () => {
    expect(fileTypeName("application/octet-stream", "agent.exe")).toBe("Windows executable (PE)")
    expect(fileTypeName("text/plain", "config.yaml")).toBe("YAML")
    expect(fileTypeName("application/octet-stream", "loot.bin")).toBe("Binary data")
  })
  it("falls back to the extension, else nothing", () => {
    expect(fileTypeName("", "dump.xyz")).toBe("XYZ file")
    expect(fileTypeName("", "noext")).toBeNull()
  })
})

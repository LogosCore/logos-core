import { describe, expect, it } from "vitest"
import { DatabaseIcon, FileCodeIcon, FileCogIcon, FileIcon, FileKeyIcon, FileTextIcon, FileVideoIcon, ImageIcon } from "lucide-react"
import { iconByName } from "./icon-by-name"

describe("iconByName", () => {
  it("knows a file's kind by its extension, in any case", () => {
    expect(iconByName("(2)e768ed85.PNG")).toBe(ImageIcon)
    expect(iconByName("21.08.26 камери.docx")).toBe(FileTextIcon)
    expect(iconByName("clip.mkv")).toBe(FileVideoIcon)
    expect(iconByName("main.go")).toBe(FileCodeIcon)
    expect(iconByName("agent.exe")).toBe(FileCogIcon)
    expect(iconByName("vault.kdbx")).toBe(FileKeyIcon)
    expect(iconByName("app.sqlite")).toBe(DatabaseIcon)
  })
  it("knows keys and secrets by their names", () => {
    expect(iconByName("id_ed25519")).toBe(FileKeyIcon)
    expect(iconByName("/home/u/.ssh/authorized_keys")).toBe(FileKeyIcon)
    expect(iconByName("C:\\Windows\\System32\\config\\SAM")).toBe(FileKeyIcon)
  })
  it("falls back to a plain file", () => {
    expect(iconByName(".bashrc")).toBe(FileIcon)
    expect(iconByName("README")).toBe(FileIcon)
    expect(iconByName("x.unknownext")).toBe(FileIcon)
  })
})

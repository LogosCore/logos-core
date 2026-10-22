import { afterEach, describe, expect, it, vi } from "vitest"

import { probeContentType } from "./wiki-file-type-probe"

function serve(contentType: string | null, status = 206) {
  const fetchMock = vi.fn(async () => new Response("x", { status, headers: contentType ? { "Content-Type": contentType } : {} }))
  vi.stubGlobal("fetch", fetchMock)
  return fetchMock
}

afterEach(() => vi.unstubAllGlobals())

describe("probeContentType", () => {
  it("asks for one byte and takes the server's type", async () => {
    const fetchMock = serve("text/plain; charset=utf-8")
    expect(await probeContentType("/api/v1/wiki/files/a")).toBe("text/plain")
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/wiki/files/a", expect.objectContaining({ headers: { Range: "bytes=0-0" } }))
  })

  it("asks once per file", async () => {
    const fetchMock = serve("video/mp4")
    await probeContentType("/api/v1/wiki/files/b")
    await probeContentType("/api/v1/wiki/files/b")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("has no answer when the server knows no more than the card", async () => {
    serve("application/octet-stream")
    expect(await probeContentType("/api/v1/wiki/files/c")).toBeNull()
    serve(null, 404)
    expect(await probeContentType("/api/v1/wiki/files/d")).toBeNull()
  })
})

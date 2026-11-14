import { createHash } from "node:crypto"
import { describe, expect, test } from "vitest"

// The vendored client is Apache Guacamole's release, byte for byte, between
// its markers (guacamole-common.js's header says where it came from).

const BEGIN = "// --- guacamole-common-js 1.6.0 all.min.js: begin ---\n"
const END = "\n// --- guacamole-common-js 1.6.0 all.min.js: end ---\nexport default Guacamole;\n"
// all.min.js of org.apache.guacamole:guacamole-common-js:1.6.0.
const RELEASE_SHA256 = "cc89f710ecc544477dbe6bfea453fab752dafa1b1ab9770f523676e7b744b44a"

describe("the vendored Guacamole client", () => {
  test("is the release's all.min.js, a module", async () => {
    const { readFileSync } = await import("node:fs")
    const { resolve } = await import("node:path")
    const file = readFileSync(resolve(__dirname, "guacamole-common.js"))
    const text = file.toString("latin1")
    const from = text.indexOf(BEGIN)
    expect(from).toBeGreaterThan(0)
    expect(text.endsWith(END)).toBe(true)
    const release = file.subarray(from + BEGIN.length, file.length - END.length)
    expect(createHash("sha256").update(release).digest("hex")).toBe(RELEASE_SHA256)
  })

  test("loads as a module (guacd-pins.test.ts checks its version is guacd's)", async () => {
    const { default: Guacamole } = await import("./guacamole-common")
    expect(Guacamole.API_VERSION).toBe("1.6.0")
    expect(typeof Guacamole.Client).toBe("function")
    expect(typeof Guacamole.WebSocketTunnel).toBe("function")
  })
})

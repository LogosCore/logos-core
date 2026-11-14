import { describe, expect, test } from "vitest"

// logos-guacd's pins (deploy/guacd/versions.env) are what the development
// stack builds (docker-compose.yml), what the chart runs (its guacd.image)
// and the release the web app's Guacamole client is: deploy/guacd/README.md.

async function read(path: string): Promise<string> {
  const { readFileSync } = await import("node:fs")
  const { resolve } = await import("node:path")
  return readFileSync(resolve(__dirname, "../../../..", path), "utf8")
}

function pins(text: string): Record<string, string> {
  return Object.fromEntries(
    text
      .split("\n")
      .filter((l) => /^[A-Z0-9_]+=/.test(l))
      .map((l) => [l.slice(0, l.indexOf("=")), l.slice(l.indexOf("=") + 1).trim()]),
  )
}

describe("logos-guacd's pins", () => {
  test("the development stack builds them, the chart runs them, the web app's client is their release", async () => {
    const p = pins(await read("deploy/guacd/versions.env"))
    const tag = `${p.GUACAMOLE_SERVER_VERSION}-freerdp${p.WITH_FREERDP}`
    expect(p.GUACAMOLE_SERVER_COMMIT).toMatch(/^[0-9a-f]{40}$/)
    expect(p.FREERDP_VERSION).toBe("3")

    const compose = await read("docker-compose.yml")
    expect(compose).toContain(`context: https://github.com/apache/guacamole-server.git#${p.GUACAMOLE_SERVER_COMMIT}`)
    for (const key of ["FREERDP_VERSION", "WITH_FREERDP", "WITH_LIBSSH2", "WITH_LIBTELNET", "WITH_LIBVNCCLIENT", "WITH_LIBWEBSOCKETS"]) {
      expect(compose, key).toContain(`${key}: "${p[key]}"`)
    }
    expect(compose).toContain(`image: logos-guacd:${tag}`)

    const values = await read("deploy/helm/logos/values.yaml")
    expect(values).toContain("repository: ghcr.io/logoscore/logos-guacd")
    expect(values).toContain(`tag: "${tag}"`)

    const { default: Guacamole } = await import("./guacamole-common")
    expect(Guacamole.API_VERSION).toBe(p.GUACAMOLE_SERVER_VERSION)
  })
})

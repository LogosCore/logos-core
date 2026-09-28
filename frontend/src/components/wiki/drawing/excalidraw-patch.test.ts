import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import { dirname, join } from "node:path"
import { describe, expect, test } from "vitest"

// patches/@excalidraw+excalidraw+0.18.1.patch makes Excalidraw colour each
// collaborator with the colour we send (`collaborator.color.background`, from
// getCursorColor — the same one the presence menu and the prose editor use).
// Unpatched, 0.18 ignores that field and derives a pale hsl(…, 100%, 83%) from
// a hash of the collaborator id, which is our per-connection Yjs client id: a
// pastel, never the person's colour, and a different one after every reload.
//
// patch-package applies the patch on install and only warns when it no longer
// matches, so a version bump can drop it without failing anything. These
// tests read the installed bundles and fail instead — re-create the patch
// against the new version (see the patch file for the one-line change).

// The package's exports map does not expose package.json, so find dist/ from
// the entry point it does export (dist/<mode>/index.js).
const require = createRequire(import.meta.url)
const dist = dirname(dirname(require.resolve("@excalidraw/excalidraw")))

describe("excalidraw collaborator colour patch", () => {
  // The production bundle is what `vite build` ships.
  test("is applied to the production bundle", () => {
    const src = readFileSync(join(dist, "prod/index.js"), "utf8")
    expect(src).toMatch(/=\(\w+,(\w+)\)=>\1\?\.color\?\.background\|\|`hsl\(/)
  })

  // The development bundle is what the dev server serves.
  test("is applied to the development bundle", () => {
    const src = readFileSync(join(dist, "dev/index.js"), "utf8")
    expect(src).toContain(
      "if (collaborator?.color?.background) {\n    return collaborator.color.background;",
    )
  })
})

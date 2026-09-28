import { describe, expect, test } from "vitest"
import { visibleFrequentIconNames } from "@/components/wiki/frequent-icons"
import { toSimpleIconName } from "@/components/wiki/simple-icon-catalog"

const DOCKER = toSimpleIconName("docker")
const UBUNTU = toSimpleIconName("ubuntu")

// The ranking comes from the server (me.frequentIcons); what is left on the
// client is deciding which of those names this bundle can still draw.
describe("visibleFrequentIconNames", () => {
  test("keeps the server's order across lucide and brand icons", () => {
    expect(visibleFrequentIconNames([UBUNTU, "Server", DOCKER])).toEqual([
      UBUNTU,
      "Server",
      DOCKER,
    ])
  })

  // The earlier bug: the read path validated names against the lucide catalog
  // only, so the row never showed a brand icon however often it was chosen.
  test("returns brand icons", () => {
    expect(visibleFrequentIconNames([DOCKER])).toEqual([DOCKER])
  })

  test("skips names that no longer resolve in either bundle", () => {
    expect(
      visibleFrequentIconNames([
        "NotARealLucideIcon",
        "Server",
        toSimpleIconName("not-a-real-brand"),
      ]),
    ).toEqual(["Server"])
  })

  test("honours the display limit, counting only visible names", () => {
    expect(visibleFrequentIconNames(["Server", DOCKER], 1)).toEqual(["Server"])
    expect(
      visibleFrequentIconNames(["NotARealLucideIcon", "Server", DOCKER], 1),
    ).toEqual(["Server"])
  })
})

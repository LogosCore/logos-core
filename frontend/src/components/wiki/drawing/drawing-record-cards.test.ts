import { describe, expect, test } from "vitest"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"

import { buildRefCard, parseRef, type CardRef, type Skeleton } from "./drawing-catalogue"
import { fitText, humanize, recordAt, recordCards, refsInPaste } from "./drawing-record-cards"

const HOST: CardRef = { kind: "host", id: "0b6f3c2e-8a51-4c7e-9d0f-1a2b3c4d5e6f" }

/** A record card as the canvas holds it: the catalogue's parts, given ids
 * and the card's group, at (x, y). */
function card(ref: CardRef, x = 0, y = 0, group = `g-${ref.id}`): ExcalidrawElement[] {
  const { elements } = buildRefCard({ ref, title: "DC01", detail: "10.0.5.10", style: "clean" })
  return elements.map(
    (sk: Skeleton, i) =>
      ({
        ...sk,
        id: `${ref.id}-${i}`,
        x: sk.x + x,
        y: sk.y + y,
        width: (sk.width as number) ?? 10,
        height: (sk.height as number) ?? 10,
        groupIds: [group],
        isDeleted: false,
      }) as unknown as ExcalidrawElement,
  )
}

describe("a record card", () => {
  test("is a catalogue card that carries its record on every part, and no element link", () => {
    const { elements } = buildRefCard({ ref: HOST, title: "DC01", detail: "", style: "sketch" })
    const tags = elements.map((el) => (el.customData as { logos: { ref?: CardRef; shape: string } }).logos)
    expect(tags.every((t) => t.ref?.id === HOST.id && t.shape === "card:server")).toBe(true)
    // Excalidraw would draw a link's handle on the corner the badges use.
    expect(elements.some((el) => el.link)).toBe(false)
  })

  test("a ref read from a scene or a text is checked", () => {
    expect(parseRef("host:abc")).toEqual({ kind: "host", id: "abc" })
    expect(parseRef({ kind: "task", id: "x-1" })).toEqual({ kind: "task", id: "x-1" })
    expect(parseRef("vm:abc")).toBeNull()
    expect(parseRef({ kind: "host", id: "a b" })).toBeNull()
    expect(parseRef({ kind: "host", id: "../../x" })).toBeNull()
    expect(parseRef(null)).toBeNull()
  })
})

describe("recordCards", () => {
  test("finds each card with its title and detail", () => {
    const task: CardRef = { kind: "task", id: "t1" }
    const cards = recordCards([...card(HOST), ...card(task, 400)])
    expect(cards.map((c) => c.key)).toEqual([`host:${HOST.id}`, "task:t1"])
    expect(cards[0].title?.text).toBe("DC01")
    expect(cards[0].detail?.text).toBe("10.0.5.10")
    expect(cards[0].partIds.length).toBeGreaterThan(4)
  })

  test("two cards for one record are two cards", () => {
    const cards = recordCards([...card(HOST, 0, 0, "a"), ...card(HOST, 400, 0, "b")])
    expect(cards).toHaveLength(2)
    expect(cards[1].title?.groupIds).toEqual(["b"])
  })

  test("a deleted card is not a card", () => {
    const deleted = card(HOST).map((el) => ({ ...el, isDeleted: true }) as ExcalidrawElement)
    expect(recordCards(deleted)).toEqual([])
  })
})

describe("recordAt", () => {
  const scene = [...card(HOST), ...card({ kind: "task", id: "t1" }, 400)]

  test("is the card under the point", () => {
    expect(recordAt(scene, { x: 100, y: 20 })).toEqual(HOST)
    expect(recordAt(scene, { x: 450, y: 20 })).toEqual({ kind: "task", id: "t1" })
    expect(recordAt(scene, { x: 350, y: 20 })).toBeNull()
  })

  test("looks through a shape drawn over the card", () => {
    const zone = { id: "zone", type: "rectangle", x: -50, y: -50, width: 1000, height: 400, groupIds: [], isDeleted: false } as unknown as ExcalidrawElement
    expect(recordAt([...scene, zone], { x: 100, y: 20 })).toEqual(HOST)
  })
})

describe("fitText", () => {
  const measure = (t: string) => t.length * 10

  test("leaves words that fit alone", () => {
    expect(fitText("DC01", 100, measure)).toBe("DC01")
  })

  test("cuts words that do not, with an ellipsis that fits", () => {
    const fitted = fitText("A very long hostname", 100, measure)
    expect(fitted.endsWith("…")).toBe(true)
    expect(measure(fitted)).toBeLessThanOrEqual(100)
    expect(fitted).toBe("A very lo…")
  })
})

test("humanize", () => {
  expect(humanize("IN_PROCESS")).toBe("In process")
  expect(humanize("API_KEY")).toBe("Api key")
})

describe("refsInPaste", () => {
  const origin = "https://logos.example"

  test("a copied chip", () => {
    const html = `<p>see <span data-wiki-host="true" data-host-id="${HOST.id}">DC01</span> and <span data-wiki-document="true" data-document-id="d1">x</span></p>`
    expect(refsInPaste(html, "see DC01 and x", origin)).toEqual([HOST, { kind: "document", id: "d1" }])
  })

  test("links to records in this app", () => {
    const text = `${origin}/tasks?task=t1\n/wiki/d1 ${origin}/findings?credential=c1&x=1 ${origin}/findings?host=h1`
    expect(refsInPaste("", text, origin)).toEqual([
      { kind: "task", id: "t1" },
      { kind: "document", id: "d1" },
      { kind: "credential", id: "c1" },
      { kind: "host", id: "h1" },
    ])
  })

  test("a sentence, another site, or a link to no record stays text", () => {
    expect(refsInPaste("", `see ${origin}/tasks?task=t1`, origin)).toEqual([])
    expect(refsInPaste("", "https://evil.example/tasks?task=t1", origin)).toEqual([])
    expect(refsInPaste("", `${origin}/tasks`, origin)).toEqual([])
    expect(refsInPaste("", "", origin)).toEqual([])
  })
})

import { describe, expect, it } from "vitest"
import { getSchema } from "@tiptap/core"
import StarterKit from "@tiptap/starter-kit"
import { EditorState, TextSelection } from "@tiptap/pm/state"
import { currentHeading, headingLevelDecorations } from "./wiki-heading-level"

const schema = getSchema([StarterKit])

// doc: <p>intro</p><h3>Scope</h3><p>body</p>
const doc = schema.node("doc", null, [
  schema.node("paragraph", null, [schema.text("intro")]),
  schema.node("heading", { level: 3 }, [schema.text("Scope")]),
  schema.node("paragraph", null, [schema.text("body")]),
])
const HEADING_POS = 7 // after <p>intro</p> (1 + 5 + 1)

function at(from: number, to = from) {
  return EditorState.create({ doc, selection: TextSelection.create(doc, from, to) })
}

describe("currentHeading", () => {
  it("finds the heading the caret is in, with its level", () => {
    expect(currentHeading(at(HEADING_POS + 3))).toEqual({ pos: HEADING_POS, level: 3 })
  })

  it("is null in a paragraph", () => {
    expect(currentHeading(at(2))).toBeNull()
  })

  // Which heading would a level change apply to?
  it("is null for a selection that leaves the heading", () => {
    expect(currentHeading(at(HEADING_POS + 2, HEADING_POS + 10))).toBeNull()
  })
})

describe("headingLevelDecorations", () => {
  it("puts one badge at the start of the heading's text", () => {
    const set = headingLevelDecorations(at(HEADING_POS + 3), true)
    const found = set.find()
    expect(found).toHaveLength(1)
    expect(found[0].from).toBe(HEADING_POS + 1)
    expect(found[0].spec.key).toBe(`heading-level-${HEADING_POS}-3`)
  })

  it("draws nothing outside a heading", () => {
    expect(headingLevelDecorations(at(2), true).find()).toHaveLength(0)
  })

  // Viewers and the print page render the editor read-only.
  it("draws nothing while the editor is read-only", () => {
    expect(headingLevelDecorations(at(HEADING_POS + 3), false).find()).toHaveLength(0)
  })
})

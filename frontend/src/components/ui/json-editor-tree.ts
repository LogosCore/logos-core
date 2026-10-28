import type { SyntaxNode, Tree } from "@lezer/common"
import { childPointer } from "@/lib/json-edit"

// Where things are in a JSON document, read from its syntax tree (the JSON
// editor's): the span of each property by JSON Pointer, the pointer of an
// object, the first syntax error. Kept apart from the editor because none of
// it needs a page, so it can be tested on its own.

/** Where a property sits in the text. */
export interface PropertySpan {
  /** The whole property: key, colon and value. */
  from: number
  to: number
  keyFrom: number
  keyTo: number
  /** Its value; at the end of the key when the value is missing. */
  valueFrom: number
  valueTo: number
  /** The value's node: Object, Array, String, Number, True, False or Null; "" when missing. */
  valueType: string
}

const VALUES = new Set(["Object", "Array", "String", "Number", "True", "False", "Null"])

/** A PropertyName node's key, decoded; as written when it does not decode. */
export function keyOf(name: SyntaxNode, doc: string): string {
  const raw = doc.slice(name.from, name.to)
  try {
    const key: unknown = JSON.parse(raw)
    if (typeof key === "string") return key
  } catch {
    // An unterminated key while it is typed.
  }
  return raw.replace(/^"/, "").replace(/"$/, "")
}

/**
 * Every property's span by its pointer, array items' too (with no key), and
 * the top value's as "". A key written twice keeps its first place.
 */
export function propertySpans(tree: Tree, doc: string): Map<string, PropertySpan> {
  const out = new Map<string, PropertySpan>()
  const top = topValue(tree)
  if (!top) return out
  out.set("", bare(top))
  walk(top, "", doc, out)
  return out
}

function topValue(tree: Tree): SyntaxNode | null {
  for (let c = tree.topNode.firstChild; c; c = c.nextSibling) if (VALUES.has(c.name)) return c
  return null
}

function bare(node: SyntaxNode): PropertySpan {
  return {
    from: node.from,
    to: node.to,
    keyFrom: node.from,
    keyTo: node.from,
    valueFrom: node.from,
    valueTo: node.to,
    valueType: node.name,
  }
}

function walk(node: SyntaxNode, pointer: string, doc: string, out: Map<string, PropertySpan>) {
  if (node.name === "Object") {
    for (let p = node.firstChild; p; p = p.nextSibling) {
      if (p.name !== "Property") continue
      const name = p.getChild("PropertyName")
      if (!name) continue
      const value = valueOf(p)
      const at = childPointer(pointer, keyOf(name, doc))
      if (!out.has(at)) {
        out.set(at, {
          from: p.from,
          to: p.to,
          keyFrom: name.from,
          keyTo: name.to,
          valueFrom: value?.from ?? name.to,
          valueTo: value?.to ?? name.to,
          valueType: value?.name ?? "",
        })
      }
      if (value) walk(value, at, doc, out)
    }
  } else if (node.name === "Array") {
    let i = 0
    for (let c = node.firstChild; c; c = c.nextSibling) {
      if (!VALUES.has(c.name)) continue
      const at = childPointer(pointer, i++)
      out.set(at, bare(c))
      walk(c, at, doc, out)
    }
  }
}

function valueOf(property: SyntaxNode): SyntaxNode | null {
  for (let c = property.lastChild; c; c = c.prevSibling) if (VALUES.has(c.name)) return c
  return null
}

/** The pointer of an Object or Array node; null when it is not inside a value. */
export function pointerOfNode(node: SyntaxNode, doc: string): string | null {
  const path: string[] = []
  let n: SyntaxNode = node
  for (;;) {
    const parent = n.parent
    if (!parent || parent.name === "JsonText") break
    if (parent.name === "Property") {
      const name = parent.getChild("PropertyName")
      const object = parent.parent
      if (!name || !object) return null
      path.unshift(keyOf(name, doc))
      n = object
    } else if (parent.name === "Array") {
      let i = 0
      for (let c = parent.firstChild; c && c.from < n.from; c = c.nextSibling) if (VALUES.has(c.name)) i++
      path.unshift(String(i))
      n = parent
    } else {
      n = parent
    }
  }
  return path.reduce<string>((p, token) => childPointer(p, token), "")
}

/** The keys an Object node has, decoded. */
export function keysOf(object: SyntaxNode, doc: string): Set<string> {
  const keys = new Set<string>()
  for (let p = object.firstChild; p; p = p.nextSibling) {
    const name = p.name === "Property" ? p.getChild("PropertyName") : null
    if (name) keys.add(keyOf(name, doc))
  }
  return keys
}

/** Where the document's first syntax error is; null when it has none. */
export function firstErrorAt(tree: Tree): number | null {
  let at: number | null = null
  tree.iterate({
    enter: (n) => {
      if (at !== null) return false
      if (n.type.isError) {
        at = n.from
        return false
      }
      return undefined
    },
  })
  return at
}

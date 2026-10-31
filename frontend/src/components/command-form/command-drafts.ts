import {
  initialParamDrafts,
  rfc3339ToLocalInput,
  type Draft,
  type FieldDef,
} from "@/components/modules/management/field-values"

// A command form's fields from text typed on a command line: each param's
// text as its field's draft, as typed. A text that does not read as the
// field's type stays in the field, for its error to show there, rather
// than being dropped.

/** A field's draft from the text typed for it. */
export function draftFromText(field: FieldDef, text: string): Draft {
  const parts = () =>
    text
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s !== "")
  switch (field.type) {
    case "bool": {
      const t = text.toLowerCase()
      if (["true", "yes", "on", "1"].includes(t)) return { kind: "bool", value: true }
      if (["false", "no", "off", "0"].includes(t)) return { kind: "bool", value: false }
      return { kind: "bool", value: null }
    }
    case "string_list":
      return { kind: "list", items: parts() }
    case "enum_list":
      return { kind: "set", values: parts() }
    case "string_map":
      return {
        kind: "map",
        entries: parts().map((pair) => {
          const eq = pair.indexOf("=")
          return eq < 0 ? { key: pair, value: "" } : { key: pair.slice(0, eq).trim(), value: pair.slice(eq + 1).trim() }
        }),
      }
    case "bytes":
      return { kind: "bytes", text, unit: "B" }
    case "datetime":
      // The form's field takes local time; text that is no time stays as typed.
      return { kind: "text", text: Number.isNaN(Date.parse(text)) ? text : rfc3339ToLocalInput(text) }
  }
  return { kind: "text", text }
}

/** A form's drafts: the fields' defaults, and over them what was typed. */
export function draftsFromTexts(fields: readonly FieldDef[], texts: ReadonlyMap<string, string>): Record<string, Draft> {
  const drafts = initialParamDrafts(fields)
  for (const f of fields) {
    const text = texts.get(f.key)
    if (text !== undefined) drafts[f.key] = draftFromText(f, text)
  }
  return drafts
}

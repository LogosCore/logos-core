import { acceptText } from "@/components/builds/build-values"
import { formatValue, type FieldDef } from "./field-values"

/**
 * What a schema field's row says after its key and type: its default, the
 * options it takes, or the files it accepts.
 */
export function fieldNotes(f: FieldDef): string[] {
  const notes: string[] = []
  if (f.default !== null && f.default !== undefined) {
    // A secret's default is part of the schema, not a stored secret.
    notes.push(`default ${formatValue(f.type === "secret" ? "string" : f.type, f.default, f.unit)}`)
  }
  if ((f.type === "enum" || f.type === "enum_list") && f.options && f.options.length > 0) {
    notes.push(f.options.map((o) => o.value).join(", "))
  }
  if (f.type === "file") notes.push(acceptText(f))
  return notes
}

/** Connection schema versions: "v2", "v1, v2". */
export function versionsText(versions: readonly number[]): string {
  return versions.map((v) => `v${v}`).join(", ")
}

/** Channel kinds with the connection schema versions read: "http v2 · dns v1, v2". */
export function channelsText(channels: readonly { moduleName: string; versions: readonly number[] }[]): string {
  return channels.map((c) => `${c.moduleName} ${versionsText(c.versions)}`).join(" · ")
}

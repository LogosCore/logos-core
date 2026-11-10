import type { ReactNode } from "react"
import { CredentialValueNames } from "@/components/findings/credential-field-control"
import type { IdRow } from "@/components/ui/info-card"
import { cn } from "@/lib/utils"
import { formatValue, type FieldDef } from "@/components/modules/management/field-values"
import type { ValueRow } from "@/components/builds/build-record"

// A build's values as an InfoCard's rows: the Overview's (the build's own)
// and the Connections tab's (each channel's settings, the factory's values
// for it).

/**
 * Values as rows: each key, its value (dimmed at its default, "not set" for
 * none; a file by its name), and under it the field's label.
 */
export function rowsOf(rows: readonly ValueRow[], fileNames: ReadonlyMap<string, string>): IdRow[] {
  return rows.map((r) => ({
    id: r.key,
    content: (
      <>
        <div className={cn("whitespace-pre-line", (r.value === undefined || r.isDefault) && "text-muted-foreground")}>
          {valueText(r, fileNames)}
        </div>
        <div className="text-xs text-muted-foreground">
          {[r.field ? r.field.label : "no longer a field of the factory", r.isDefault ? "default" : ""]
            .filter(Boolean)
            .join(" · ")}
        </div>
      </>
    ),
  }))
}

function valueText(r: ValueRow, fileNames: ReadonlyMap<string, string>): ReactNode {
  if (r.value === undefined) return "not set"
  const f: FieldDef | null = r.field
  if (f?.type === "file" && typeof r.value === "string") return fileNames.get(r.value) ?? r.value
  if (f?.type === "credential" || f?.type === "credential_list") return <CredentialValueNames value={r.value} />
  return formatValue(f?.type ?? "", r.value, f?.unit)
}

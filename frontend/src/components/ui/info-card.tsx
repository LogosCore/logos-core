import type { ReactNode } from "react"
import { TableBody, TableCell, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"

// Read-only cards of a record's parts (a factory's targets, a build's
// values): a header with the card's title, a note beside it (a count, a
// version) and actions at its end, over a two-column table without column
// headings. Its rows are an id and what is said of it (IdRows), a label and
// a value (LabelRow), a row across heading the rows under it (SectionRow),
// or a line across (NoteRow). Lay several out with CardColumns.

export function InfoCard({
  title,
  note,
  actions,
  children,
}: {
  title: ReactNode
  /** Beside the title, muted: a count, a version. */
  note?: ReactNode
  /** At the header's end: buttons. */
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="min-w-0 rounded-xl border bg-card">
      <div className="flex min-h-10 items-center gap-2 border-b px-4 py-2">
        <h3 className="flex min-w-0 items-baseline gap-2 text-sm font-semibold">
          {title}
          {note !== undefined && <span className="text-xs font-normal text-muted-foreground tabular-nums">{note}</span>}
        </h3>
        {actions && <div className="ml-auto flex items-center gap-1">{actions}</div>}
      </div>
      {/* Fixed widths, set here: a first row across both columns must not
          make them halves. */}
      <table className="w-full table-fixed text-sm">
        <colgroup>
          <col className="w-40" />
          <col />
        </colgroup>
        <TableBody>{children}</TableBody>
      </table>
    </section>
  )
}

/** One row of IdRows: an id, and what is said of it. */
export interface IdRow {
  id: string
  content: ReactNode
}

// The first column's cell; the column is as wide in every card (colgroup).
const FIRST = "px-4 py-2 align-baseline whitespace-normal"

/** Rows of an id, monospaced, and what is said of it; the id and the first line beside it share a baseline. */
export function IdRows({ rows }: { rows: readonly IdRow[] }) {
  return rows.map((r) => (
    <TableRow key={r.id} className="hover:bg-transparent">
      <TableCell className={cn(FIRST, "break-all")}>
        <code className="font-mono text-xs">{r.id}</code>
      </TableCell>
      <TableCell className="px-4 py-2 align-baseline break-words whitespace-normal">{r.content}</TableCell>
    </TableRow>
  ))
}

/** A row of a label, muted, and its value. */
export function LabelRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <TableRow className="hover:bg-transparent">
      <TableCell className={cn(FIRST, "text-muted-foreground")}>{label}</TableCell>
      <TableCell className="px-4 py-2 align-baseline break-words whitespace-normal">{children}</TableCell>
    </TableRow>
  )
}

/** A row across the table, heading the rows under it. */
export function SectionRow({ title, note }: { title: ReactNode; note?: ReactNode }) {
  return (
    <TableRow className="bg-muted/40 hover:bg-muted/40">
      <TableCell colSpan={2} className="px-4 py-1.5 text-xs whitespace-normal text-muted-foreground">
        <span className="font-medium">{title}</span>
        {note !== undefined && <span> · {note}</span>}
      </TableCell>
    </TableRow>
  )
}

/** A line across the table: what a card has instead of rows, or a note on them. */
export function NoteRow({ children }: { children: ReactNode }) {
  return (
    <TableRow className="hover:bg-transparent">
      <TableCell colSpan={2} className="px-4 py-2 whitespace-pre-line text-muted-foreground">
        {children}
      </TableCell>
    </TableRow>
  )
}

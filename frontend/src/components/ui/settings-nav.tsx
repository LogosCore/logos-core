import { cn } from "@/lib/utils"

/** A section of a settings form, as the side list shows it. */
export interface SettingsNavSection {
  id: string
  title: string
  /** A marker: something wrong in it, or values set. */
  state: "error" | "set" | null
  /** One of the section above's parts (a build's connection), indented under it. */
  sub?: boolean
}

/**
 * The side list of a settings form's sections (the endpoint and build
 * editors), on wide enough screens: the one being read highlighted, a dot for
 * one with errors or values, a click to go there. Pair it with
 * useActiveSection.
 */
export function SettingsNav({
  sections,
  active,
  onPick,
}: {
  sections: SettingsNavSection[]
  active: string | null
  onPick: (id: string) => void
}) {
  return (
    <nav className="sticky top-6 hidden w-40 shrink-0 self-start md:block" aria-label="Sections">
      <ul className="flex flex-col gap-0.5">
        {sections.map((s) => (
          <li key={s.id}>
            <button
              type="button"
              onClick={() => onPick(s.id)}
              title={s.sub ? s.title : undefined}
              className={cn(
                "flex w-full min-w-0 items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-sm",
                s.sub && "py-1 pl-5 text-xs",
                active === s.id ? "bg-foreground/10 font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              <span className="truncate">{s.title}</span>
              {s.state && (
                <span
                  className={cn("size-1.5 shrink-0 rounded-full", s.state === "error" ? "bg-destructive" : "bg-foreground/50")}
                  aria-label={s.state === "error" ? "has errors" : "has values"}
                />
              )}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  )
}

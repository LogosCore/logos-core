import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { SettingsSection } from "@/components/ui/settings-rows"
import { groupCommands, type Command } from "@/components/builds/build-values"

/**
 * The build form's commands: what the minion will be able to do, and nothing
 * else. One checkbox per command of the target, under the factory's
 * headings; the required ones stay checked. onChange gets the picks as they
 * are: the form keeps them to the target's, its required ones added.
 */
export function CommandPicker({
  id,
  title,
  commands,
  picked,
  defaults,
  onChange,
  error,
  disabled,
}: {
  /** The section's id, for the side list to scroll to. */
  id: string
  title: string
  /** The target's commands, in the factory's order. */
  commands: readonly Command[]
  picked: readonly string[]
  /** What a new build of the target starts with. */
  defaults: readonly string[]
  onChange: (picked: string[]) => void
  error?: string
  disabled: boolean
}) {
  const on = new Set(picked)
  const groups = groupCommands(commands)
  const optional = commands.some((c) => !c.required)
  return (
    <SettingsSection id={id} title={title}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm">
        <span className="text-muted-foreground">
          {commands.length === 0
            ? "This target has no commands."
            : `${picked.length} of ${commands.length} picked: the minion can do these and nothing else.`}
        </span>
        {optional && (
          <span className="ml-auto flex items-center gap-1">
            <Button type="button" variant="ghost" size="xs" disabled={disabled} onClick={() => onChange(commands.map((c) => c.id))}>
              All
            </Button>
            <Button type="button" variant="ghost" size="xs" disabled={disabled} onClick={() => onChange([...defaults])}>
              Defaults
            </Button>
            <Button type="button" variant="ghost" size="xs" disabled={disabled} onClick={() => onChange([])}>
              Required only
            </Button>
          </span>
        )}
      </div>
      {groups.map((g) => (
        <div key={g.group} role="group" aria-label={g.group} className="py-3">
          {groups.length > 1 && <h3 className="mb-2 text-xs font-medium text-muted-foreground">{g.group}</h3>}
          <div className="grid gap-x-6 gap-y-3 @3xl/settings:grid-cols-2">
            {g.items.map((c) => (
              <label key={c.id} className="flex items-start gap-2 text-sm">
                <Checkbox
                  className="mt-0.5"
                  checked={c.required || on.has(c.id)}
                  disabled={disabled || c.required}
                  onCheckedChange={(checked) => onChange(checked ? [...picked, c.id] : picked.filter((p) => p !== c.id))}
                />
                <span className="flex min-w-0 flex-col">
                  <span className="flex flex-wrap items-baseline gap-x-2">
                    <span>{c.label}</span>
                    <code className="font-mono text-xs text-muted-foreground">{c.id}</code>
                    {c.required && <span className="text-xs text-muted-foreground">always built in</span>}
                  </span>
                  {c.description && <span className="text-xs text-muted-foreground">{c.description}</span>}
                </span>
              </label>
            ))}
          </div>
        </div>
      ))}
      {error && <p className="py-3 text-sm text-destructive">{error}</p>}
    </SettingsSection>
  )
}

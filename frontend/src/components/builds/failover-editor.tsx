import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import {
  DEFAULT_FAILOVER,
  failoverAuto,
  type ConnectionFailover,
  type FailoverPolicy,
} from "./failover-values"

// The build form's failover settings (failover-values.ts): the build's
// policy, a section of its own after the connections, and each
// connection's own two.

/**
 * A switch as the form's other on/off fields have it: with On or Off beside
 * it, as tall as an input, so it sits level with its label.
 */
function OnOff({
  id,
  checked,
  onChange,
  disabled,
}: {
  id: string
  checked: boolean
  onChange: (on: boolean) => void
  disabled: boolean
}) {
  return (
    <div className="flex min-h-8 items-center gap-2">
      <Switch id={id} checked={checked} onCheckedChange={onChange} disabled={disabled} />
      <span className="text-sm text-muted-foreground">{checked ? "On" : "Off"}</span>
    </div>
  )
}

/** A count as an input shows it: "" for one that is not a number yet. */
const countText = (n: number | undefined) => (n !== undefined && Number.isFinite(n) ? String(n) : "")
/** A count as typed: NaN for text that is not one, so the form marks it. */
const countOf = (text: string) => (text.trim() === "" ? Number.NaN : Number(text))

/**
 * The build's failover policy. With honored false the factory does not say
 * its minions apply it: the settings are still kept and sent, and it may
 * ignore them.
 */
export function FailoverSettings({
  value,
  onChange,
  errors,
  disabled,
  honored,
  factory,
  connections,
  id,
}: {
  value: FailoverPolicy
  onChange: (f: FailoverPolicy) => void
  errors: Record<string, string>
  disabled: boolean
  honored: boolean
  factory: string
  /** How many connections the build has: failover needs two. */
  connections: number
  /** The section's id, for the side list. */
  id?: string
}) {
  const set = (patch: Partial<FailoverPolicy>) => onChange({ ...value, ...patch })
  const duration = (key: "fail_for" | "cycle_pause" | "failback_after", label: string, help: string) => (
    <SettingRow id={`failover-${key}`} label={label} fieldKey={key} help={help} error={errors[`failover.${key}`]}>
      <Input
        id={`failover-${key}`}
        className="max-w-40 font-mono"
        value={value[key]}
        onChange={(e) => set({ [key]: e.target.value })}
        placeholder={DEFAULT_FAILOVER[key]}
        spellCheck={false}
        autoComplete="off"
        disabled={disabled || !value.enabled}
        aria-invalid={!!errors[`failover.${key}`] || undefined}
      />
    </SettingRow>
  )
  return (
    <SettingsSection
      id={id}
      title="Failover"
      intro={
        <>
          <p>
            How the minion moves to its next connection on its own when the one in use keeps failing, and back to the
            first once it answers again. Each move shows on the session.
            {connections < 2 && " It needs two connections or more."}
          </p>
          {!honored && (
            <p className="rounded-md bg-amber-500/10 px-3 py-2 text-amber-800 dark:text-amber-300">
              {factory} does not say its minions apply failover settings: they are kept with the build and sent to the
              factory, which may ignore them.
            </p>
          )}
        </>
      }
    >
      <SettingRow
        id="failover-enabled"
        label="Fail over"
        fieldKey="enabled"
        help="Off: the minion stays on its connection until an operator switches it."
      >
        <OnOff id="failover-enabled" checked={value.enabled} onChange={(on) => set({ enabled: on })} disabled={disabled} />
      </SettingRow>
      <SettingRow
        id="failover-fail_after"
        label="Move after"
        fieldKey="fail_after"
        help="Failed exchanges in a row on a connection before the minion moves to the next: no answer, a timeout, a TLS or HTTP error. A connection may set its own."
        error={errors["failover.fail_after"]}
      >
        <div className="flex items-center gap-2">
          <Input
            id="failover-fail_after"
            className="w-24"
            inputMode="numeric"
            value={countText(value.fail_after)}
            onChange={(e) => set({ fail_after: countOf(e.target.value) })}
            placeholder={String(DEFAULT_FAILOVER.fail_after)}
            disabled={disabled || !value.enabled}
            aria-invalid={!!errors["failover.fail_after"] || undefined}
          />
          <span className="text-sm text-muted-foreground">failures</span>
        </div>
      </SettingRow>
      {duration(
        "fail_for",
        "Failing for at least",
        'Also wait until the connection has been failing this long, from the first of those failures, so a short blip that piles up quick retries does not move the minion. "0s": the count alone decides.',
      )}
      <SettingRow
        id="failover-wrap"
        label="Round the list"
        fieldKey="wrap"
        help="After the last connection, the first again. Off: the minion stays on the last and keeps trying it."
      >
        <OnOff id="failover-wrap" checked={value.wrap} onChange={(on) => set({ wrap: on })} disabled={disabled || !value.enabled} />
      </SettingRow>
      {duration(
        "cycle_pause",
        "Pause after a failed round",
        "When every connection the minion may move to has failed in a row, it waits this long before the next round, rather than hammering them all.",
      )}
      {duration(
        "failback_after",
        "Back to the first after",
        'After this long on a later connection, the minion tries its first one again, once: it moves back if it answers, and tries again after the same time if not. "0s": it stays until an operator switches it.',
      )}
    </SettingsSection>
  )
}

/** A connection's own failover settings, under its card. */
export function ConnectionFailoverSettings({
  idPrefix,
  connKind,
  value,
  onChange,
  error,
  disabled,
  buildFailAfter,
}: {
  idPrefix: string
  /** The connection kind the build gets: a listening one is left out by default. */
  connKind: string
  value: ConnectionFailover | undefined
  onChange: (f: ConnectionFailover | undefined) => void
  error?: string
  disabled: boolean
  buildFailAfter: number
}) {
  const set = (patch: ConnectionFailover) => {
    const next = { ...value, ...patch }
    const clean: ConnectionFailover = {}
    if (next.auto !== undefined) clean.auto = next.auto
    if (next.fail_after !== undefined) clean.fail_after = next.fail_after
    onChange(clean.auto === undefined && clean.fail_after === undefined ? undefined : clean)
  }
  const auto = failoverAuto(connKind, value)
  const listening = connKind === "bind-persistent"
  return (
    <SettingsSection title="Failover" sub>
      <SettingRow
        id={`${idPrefix}-auto`}
        label="Failover may move here"
        fieldKey="auto"
        help={
          listening
            ? "Off by default: core has to dial a minion that listens, so a move here needs a target for the session. An operator's switch can always move it here."
            : "Off: failover passes this connection by, keeping it for an operator's switch (a reserve you do not want used on its own)."
        }
      >
        <OnOff
          id={`${idPrefix}-auto`}
          checked={auto}
          // Back at the kind's default: no setting of its own.
          onChange={(on) => set({ auto: on === !listening ? undefined : on })}
          disabled={disabled}
        />
      </SettingRow>
      <SettingRow
        id={`${idPrefix}-fail_after`}
        label="Move off after"
        fieldKey="fail_after"
        help="Failures in a row before the minion moves off this connection. Empty: the build's."
        error={error}
      >
        <div className="flex items-center gap-2">
          <Input
            id={`${idPrefix}-fail_after`}
            className="w-24"
            inputMode="numeric"
            value={countText(value?.fail_after)}
            onChange={(e) => set({ fail_after: e.target.value.trim() === "" ? undefined : countOf(e.target.value) })}
            placeholder={Number.isFinite(buildFailAfter) ? String(buildFailAfter) : ""}
            disabled={disabled}
            aria-invalid={!!error || undefined}
          />
          <span className="text-sm text-muted-foreground">failures</span>
        </div>
      </SettingRow>
    </SettingsSection>
  )
}

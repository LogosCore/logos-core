import { useMemo, useState } from "react"
import { BookmarkIcon, BracesIcon, ListIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { LazyJsonEditor } from "@/components/ui/lazy-json-editor"
import { EndpointSettings } from "@/components/endpoints/endpoint-settings"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { FieldRow } from "@/components/builds/build-field-row"
import { ConnectionFailoverSettings } from "@/components/builds/failover-editor"
import { DEFAULT_FAILOVER } from "@/components/builds/failover-values"
import {
  channelUnusable,
  connectionDraft,
  effectiveKind,
  factoryKindsByName,
  fillFromEndpoint,
  paramFields,
  usableKinds,
  withKind,
  type ConnectionDraft,
} from "@/components/builds/build-values"
import { ConnectionPickerDialog } from "@/components/builds/connection-picker-dialog"
import { connectionOfDraft, draftOfConnection, readConnectionJSON, type ConnectionJSON } from "@/components/builds/connection-value"
import { useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelEndpointCount, useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { choiceDetail, choiceTitle, connectionJSON } from "./connection-choices"
import { useConnectionChoices } from "./use-connection-choices"
import { useSessionFormId } from "./session-form-context"

// A command's connection param (a structured param of the connection
// format: edit_connection's connection) as the build form edits a
// connection: its channel, its kind, every setting of the
// channel for that kind and every parameter the session's factory has for
// it, set or not, and its failover. What it edits is the param's JSON,
// written back on every change, so the command form checks and sends it as
// any structured value; Edit as JSON shows that text itself.

export interface ConnectionFieldProps {
  /** The param's text: the connection's JSON. */
  text: string
  onChange: (text: string) => void
  disabled: boolean
  idPrefix: string
  label: string
}

export default function ConnectionField({ text, onChange, disabled, idPrefix, label }: ConnectionFieldProps) {
  const sessionId = useSessionFormId() ?? ""
  const session = useMinionSession(sessionId).data?.minionSession
  const factoryKinds = useFactoryKinds()
  const channelKinds = useChannelKinds()
  const choices = useConnectionChoices()
  const factory = useMemo(
    () => (session ? factoryKindsByName(factoryKinds.data?.factoryKinds ?? []).get(session.moduleName) : undefined),
    [factoryKinds.data, session],
  )
  const channels = useMemo(() => kindsByName(channelKinds.data?.channelKinds ?? []), [channelKinds.data])
  const target = factory?.targets.find((t) => t.id === session?.target)
  const channelIcons = useChannelIcons()
  // The channels the session's target speaks, and the kinds its minions use
  // through them: what a saved endpoint may fill the connection from, as
  // in the build form.
  const speaks = (target?.channels ?? []).map((c) => ({ name: c.moduleName, why: channelUnusable(target, c.moduleName, channels) }))
  const usable = speaks.filter((c) => c.why === null).map((c) => c.name)
  const usableConnKinds = [...new Set(usable.flatMap((m) => usableKinds(target, channels.get(m), m)))]
  const endpointCount = useChannelEndpointCount(session?.operationId ?? "", usable, usableConnKinds)
  const [picking, setPicking] = useState(false)

  // What the form does not edit, kept from the value it started from.
  const [kept, setKept] = useState<ConnectionJSON | null>(() => readConnectionJSON(text))
  const [draft, setDraft] = useState<ConnectionDraft | null>(null)
  const [asJSON, setAsJSON] = useState(false)
  // The text it wrote last: another (a connection picked in the form, the
  // JSON edited) starts the form over from it.
  const [written, setWritten] = useState(text)
  if (text !== written) {
    setWritten(text)
    setKept(readConnectionJSON(text))
    setDraft(null)
  }

  if (!factory) {
    // The factory's schema is still coming (or the session is gone): its JSON meanwhile.
    return <JsonText text={text} onChange={onChange} disabled={disabled} label={label} />
  }
  // The form starts from the param's value the first time it shows.
  const current = draft ?? (kept ? draftOfConnection(factory, channels.get(kept.channel), kept, target) : null)

  const write = (c: ConnectionDraft, from: ConnectionJSON | null = kept) => {
    setDraft(c)
    const { value } = connectionOfDraft(factory, channels.get(c.moduleName), c, target, from)
    const next = JSON.stringify(value, null, 2)
    setWritten(next)
    onChange(next)
  }
  const startFrom = (v: ConnectionJSON) => {
    setKept(v)
    write(draftOfConnection(factory, channels.get(v.channel), v, target), v)
  }
  const errors = current ? connectionOfDraft(factory, channels.get(current.moduleName), current, target, kept).errors : {}

  const modules = (target?.channels ?? []).map((c) => c.moduleName).filter((m) => channels.has(m))
  // A saved endpoint's kind and settings in the form (the factory's
  // parameters kept where the kind keeps them).
  const fromEndpoint = (ep: Parameters<typeof fillFromEndpoint>[3] & { moduleName: string }) => {
    setPicking(false)
    const ch = channels.get(ep.moduleName)
    if (!ch) return
    const base =
      current?.moduleName === ep.moduleName
        ? current
        : connectionDraft(factory, ch, { moduleName: ep.moduleName }, target)
    write(fillFromEndpoint(base, ch, factory, ep))
  }
  const savedEndpoint = endpointCount !== 0 && (
    <Button type="button" variant="outline" size="sm" className="shrink-0" disabled={disabled} onClick={() => setPicking(true)}>
      <BookmarkIcon />
      From saved endpoint…
    </Button>
  )
  const picker = (
    <ConnectionPickerDialog
      open={picking}
      onClose={() => setPicking(false)}
      title="Fill the connection from an endpoint"
      description={`Its kind and settings become the endpoint's. The ${factory.name} parameters stay where the kind keeps them.`}
      operationId={session?.operationId ?? ""}
      kinds={usable}
      connectionKinds={usableConnKinds}
      channels={channels}
      channelIcons={channelIcons}
      blank={speaks}
      onEndpoint={fromEndpoint}
      onBlank={(m) => {
        setPicking(false)
        write(connectionDraft(factory, channels.get(m), { moduleName: m }, target))
      }}
    />
  )
  const fill = choices.some((c) => c.value) && (
    <Select
      value={null}
      onValueChange={(v) => {
        const c = choices.find((x) => String(x.place) === v)
        const json = c?.value ? readConnectionJSON(connectionJSON(c.value)) : null
        if (json) startFrom(json)
      }}
      disabled={disabled}
    >
      <SelectTrigger className="w-full" aria-label="Fill from one of the session's connections">
        <SelectValue placeholder="Fill from one of the session's connections" />
      </SelectTrigger>
      <SelectContent>
        {choices.map((c) => (
          <SelectItem key={c.place} value={String(c.place)} description={choiceDetail(c)}>
            {choiceTitle(c)}
            {c.inUse && <span className="text-xs text-muted-foreground"> · in use</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
  // Where the connection can start from: one of the session's, a saved endpoint.
  const sources = (
    <>
      {(fill || savedEndpoint) && (
        <div className="flex items-center gap-2">
          {fill && <div className="min-w-0 flex-1">{fill}</div>}
          {savedEndpoint}
        </div>
      )}
      {picker}
    </>
  )
  const toggle = (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="self-end"
      disabled={disabled}
      onClick={() => {
        if (asJSON) {
          // Back to the form: from the text as it stands now.
          const v = readConnectionJSON(text)
          setKept(v)
          setDraft(null)
        }
        setAsJSON(!asJSON)
      }}
    >
      {asJSON ? <ListIcon /> : <BracesIcon />}
      {asJSON ? "Edit as a form" : "Edit as JSON"}
    </Button>
  )

  if (asJSON) {
    return (
      <div className="flex flex-col gap-1">
        {sources}
        <JsonText text={text} onChange={onChange} disabled={disabled} label={label} />
        {toggle}
      </div>
    )
  }

  const ch = current ? channels.get(current.moduleName) : undefined
  const kind = current ? effectiveKind(current, target, ch) : ""
  const kinds = current ? usableKinds(target, ch, current.moduleName) : []
  const params = current ? paramFields(factory, current.moduleName, kind) : []
  const channelPicker = (
    <SettingRow id={`${idPrefix}-channel`} label="Channel" help="The channel module the connection goes through.">
      <Select
        value={current?.moduleName ?? null}
        onValueChange={(m) => {
          if (typeof m !== "string" || m === current?.moduleName) return
          write(connectionDraft(factory, channels.get(m), { moduleName: m, name: current?.name || null }, target))
        }}
        disabled={disabled}
      >
        <SelectTrigger id={`${idPrefix}-channel`} className="w-full">
          <SelectValue placeholder="Pick a channel" />
        </SelectTrigger>
        <SelectContent>
          {modules.map((m) => (
            <SelectItem key={m} value={m}>
              {m}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingRow>
  )
  const general = current && channelPicker

  return (
    <div className="flex flex-col gap-3">
      {sources}
      <div className="flex flex-col gap-6 rounded-md border px-4 pt-1 pb-4">
        {!current ? (
          <SettingsSection sub>{channelPicker}</SettingsSection>
        ) : ch ? (
          <EndpointSettings
            channel={ch}
            kind={kind}
            onKind={(k) => write(withKind(current, k, ch, factory))}
            usable={kinds}
            why={() => "This session's minion does not use it"}
            drafts={current.settings}
            onDraft={(key, d) => write({ ...current, settings: { ...current.settings, [key]: d } })}
            errorOf={(key) => errors[`params.${key}`]}
            disabled={disabled}
            idPrefix={`${idPrefix}-setting`}
            general={general}
            sub
          />
        ) : (
          <SettingsSection sub>{general}</SettingsSection>
        )}
        {current && params.length > 0 && (
          <SettingsSection title={`${factory.name} parameters`} sub>
            {params.map((f) => (
              <FieldRow
                key={f.key}
                idPrefix={`${idPrefix}-param`}
                field={f}
                draft={current.drafts[f.key]}
                onChange={(d) => write({ ...current, drafts: { ...current.drafts, [f.key]: d } })}
                error={errors[`values.${f.key}`]}
                disabled={disabled}
              />
            ))}
          </SettingsSection>
        )}
        {current && (
          <ConnectionFailoverSettings
            idPrefix={`${idPrefix}-failover`}
            connKind={kind}
            value={current.failover}
            onChange={(f) => write({ ...current, failover: f })}
            error={errors["failover.fail_after"]}
            disabled={disabled}
            buildFailAfter={DEFAULT_FAILOVER.fail_after}
          />
        )}
      </div>
      {toggle}
    </div>
  )
}

/** The param's text in the JSON editor, its height following the text. */
function JsonText({ text, onChange, disabled, label }: { text: string; onChange: (t: string) => void; disabled: boolean; label: string }) {
  const lines = text.split("\n").length
  return (
    <LazyJsonEditor
      value={text}
      onChange={onChange}
      readOnly={disabled}
      className="rounded-md border"
      style={{ height: `clamp(12rem, ${lines * 21 + 56}px, 60vh)` }}
      aria-label={label}
    />
  )
}

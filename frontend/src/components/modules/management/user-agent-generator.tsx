import { useState } from "react"
import { DicesIcon, TriangleAlertIcon, WandSparklesIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import { Choices, Chip } from "./generator-parts"
import {
  checkUserAgent,
  defaultBehind,
  describeUserAgent,
  generateUserAgent,
  parseUserAgent,
  randomChoice,
  releaseAge,
  releaseTitle,
  uaChoices,
  UA_BROWSER_LABELS,
  UA_BROWSERS_BY_OS,
  UA_OS_LABELS,
  type UABrowser,
  type UAChoice,
  type UAOS,
} from "@/lib/user-agent"

/**
 * Where the generator starts: on the field's value when it is a string it
 * makes (its version picked when still offered, else the default), else
 * Chrome on Windows at the default version.
 */
function startFrom(value: string): UAChoice {
  const p = parseUserAgent(value)
  if (p) {
    const i = uaChoices(p.browser).findIndex((r) => r.version === p.version)
    return { os: p.os, browser: p.browser, behind: i === -1 ? defaultBehind(p.browser) : i }
  }
  return { os: "windows", browser: "chrome", behind: defaultBehind("chrome") }
}

/**
 * "Generate" helper for fields with format "user_agent": pick an OS, a
 * browser and one of its newest versions, see the resulting string, and use
 * it. It opens on the field's value.
 */
export function UserAgentGenerator({
  value,
  onUse,
  disabled,
}: {
  /** The field's value: where the generator starts. */
  value: string
  onUse: (ua: string) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [choice, setChoice] = useState<UAChoice>(() => startFrom(value))
  const ua = generateUserAgent(choice)
  const browsers = UA_BROWSERS_BY_OS[choice.os]
  const versions = uaChoices(choice.browser)

  // Another browser starts at its own default version.
  const pick = (os: UAOS, browser: UABrowser) =>
    setChoice({ os, browser, behind: browser === choice.browser ? choice.behind : defaultBehind(browser) })

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) setChoice(startFrom(value))
        setOpen(next)
      }}
    >
      <PopoverTrigger
        render={
          <Button type="button" variant="outline" disabled={disabled} title="Generate a User-Agent">
            <WandSparklesIcon />
            Generate
          </Button>
        }
      />
      <PopoverContent align="end" className="w-[30rem] max-w-[calc(100vw-2rem)] gap-3 p-3">
        <Choices label="Operating system">
          {(Object.keys(UA_OS_LABELS) as UAOS[]).map((os) => (
            <Chip
              key={os}
              active={choice.os === os}
              onClick={() => pick(os, UA_BROWSERS_BY_OS[os].includes(choice.browser) ? choice.browser : UA_BROWSERS_BY_OS[os][0])}
            >
              {UA_OS_LABELS[os]}
            </Chip>
          ))}
        </Choices>
        <Choices label="Browser">
          {browsers.map((b) => (
            <Chip key={b} active={choice.browser === b} onClick={() => pick(choice.os, b)}>
              {UA_BROWSER_LABELS[b]}
            </Chip>
          ))}
        </Choices>
        <Choices label="Version">
          {versions.map((r, i) => (
            <Chip
              key={r.version}
              active={choice.behind === i}
              title={releaseTitle(r)}
              onClick={() => setChoice({ ...choice, behind: i })}
            >
              {r.version}
              <span className="text-muted-foreground"> · {releaseAge(r.date)}</span>
            </Chip>
          ))}
        </Choices>

        <code className="rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-5 break-all">{ua}</code>
        <p className="text-xs text-muted-foreground">
          Versions come from each browser&apos;s release dates. The default is the newest out for a week: most of its
          users run it by then.
        </p>

        <div className="flex items-center justify-between gap-2">
          <Button type="button" variant="ghost" size="sm" onClick={() => setChoice(randomChoice())}>
            <DicesIcon />
            Random common
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => {
              onUse(ua)
              setOpen(false)
            }}
          >
            Use
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

/**
 * Under a User-Agent field: what its value is and how far behind its
 * browser, what in it no real browser sends, and the fix (the same OS and
 * browser at the default version) when it is outdated or gives itself away.
 * Nothing for a string it does not know and finds nothing in.
 */
export function UserAgentCheck({
  value,
  isDefault,
  onUpdate,
  disabled,
}: {
  value: string
  /** The value is the module's default, shown because the field is empty. */
  isDefault?: boolean
  onUpdate: (ua: string) => void
  disabled?: boolean
}) {
  if (value.trim() === "") return null
  const check = checkUserAgent(value)
  const line = describeUserAgent(check)
  if (line === null && check.findings.length === 0) return null
  const fix = check.fix
  const warn = check.stale || check.findings.length > 0
  const browser = check.parsed ? UA_BROWSER_LABELS[check.parsed.browser] : ""
  return (
    <div className="flex flex-col gap-0.5 text-xs">
      {line !== null && (
        <div
          className={cn(
            "flex flex-wrap items-baseline gap-x-2",
            warn ? "text-amber-700 dark:text-amber-400" : "text-muted-foreground",
          )}
        >
          <span>{isDefault ? `Default: ${line}` : line}</span>
          {fix && !disabled && (
            <Button
              type="button"
              variant="link"
              size="xs"
              className="h-auto px-0 text-xs"
              title={fix.ua}
              onClick={() => onUpdate(fix.ua)}
            >
              {fix.version === check.parsed?.version ? "Rewrite it" : `Update to ${browser} ${fix.version}`}
            </Button>
          )}
        </div>
      )}
      {check.findings.map((f) => (
        <div key={f} className="flex items-baseline gap-1.5 text-amber-700 dark:text-amber-400">
          <TriangleAlertIcon className="size-3.5 shrink-0 self-center" aria-hidden />
          <span>{f}</span>
        </div>
      ))}
    </div>
  )
}

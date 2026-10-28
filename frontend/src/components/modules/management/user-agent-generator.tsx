import { useState } from "react"
import { DicesIcon, WandSparklesIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Choices, Chip } from "./generator-parts"
import {
  generateUserAgent,
  randomChoice,
  uaVersion,
  UA_BROWSER_LABELS,
  UA_BROWSERS_BY_OS,
  UA_OS_LABELS,
  type UABrowser,
  type UAChoice,
  type UAOS,
} from "@/lib/user-agent"

const BEHIND = [0, 1, 2, 3]

/**
 * "Generate" helper for fields with format "user_agent": pick an OS, a
 * browser and how recent a version, see the resulting string, and use it.
 */
export function UserAgentGenerator({
  onUse,
  disabled,
}: {
  onUse: (ua: string) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [choice, setChoice] = useState<UAChoice>({ os: "windows", browser: "chrome", behind: 0 })
  const ua = generateUserAgent(choice)
  const browsers = UA_BROWSERS_BY_OS[choice.os]

  const pickOS = (os: UAOS) =>
    setChoice((c) => ({ ...c, os, browser: UA_BROWSERS_BY_OS[os].includes(c.browser) ? c.browser : UA_BROWSERS_BY_OS[os][0] }))

  return (
    <Popover open={open} onOpenChange={setOpen}>
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
            <Chip key={os} active={choice.os === os} onClick={() => pickOS(os)}>
              {UA_OS_LABELS[os]}
            </Chip>
          ))}
        </Choices>
        <Choices label="Browser">
          {browsers.map((b: UABrowser) => (
            <Chip key={b} active={choice.browser === b} onClick={() => setChoice((c) => ({ ...c, browser: b }))}>
              {UA_BROWSER_LABELS[b]}
            </Chip>
          ))}
        </Choices>
        <Choices label="Version">
          {BEHIND.map((behind) => (
            <Chip
              key={behind}
              active={choice.behind === behind}
              onClick={() => setChoice((c) => ({ ...c, behind }))}
            >
              {uaVersion({ ...choice, behind })}
              <span className="text-muted-foreground">{behind === 0 ? " latest" : ` −${behind}`}</span>
            </Chip>
          ))}
        </Choices>

        <code className="rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-5 break-all">{ua}</code>
        <p className="text-xs text-muted-foreground">
          Versions are estimated from each browser's release schedule.
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

import { CopyIcon, PlugIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { SkillDownloadButton, AllSkillsLink } from "@/components/agents/skill-download-button"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { mcpEndpointUrl } from "@/constants/mcp"

/**
 * Everything needed to get an agent talking to this server, on demand.
 *
 * A sheet rather than a block on the page: an operator running a dozen agents
 * needs this once and then never again, and a permanent tutorial section would
 * push the grid they actually came for below the fold. The empty state carries
 * the same material inline for someone who has not started yet.
 */
export function ConnectAgentSheet() {
  return (
    <Sheet>
      <SheetTrigger render={<Button variant="outline" />}>
        <PlugIcon className="size-4" />
        Connect an agent
      </SheetTrigger>
      {/* See agent-form-sheet: the default width is set behind a data-side
          selector, so a plain sm:max-w-* loses to it. */}
      <SheetContent className="w-full overflow-y-auto sm:max-w-lg!">
        <SheetHeader>
          <SheetTitle>Connect an agent</SheetTitle>
          <SheetDescription>
            Two steps: point an MCP client at this server with an agent key, then
            give the agent the skill so it knows how to work here.
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-6 px-4 pb-6">
          <EndpointStep />
          <SkillStep />
        </div>
      </SheetContent>
    </Sheet>
  )
}

function EndpointStep() {
  const config = mcpClientConfig()

  return (
    <section className="space-y-2">
      <StepHeading n={1} title="Point the client at this server" />
      <p className="text-xs text-muted-foreground">
        Pass the token as a bearer credential. Any MCP client works — this is the
        shape most of them use.
      </p>
      <Snippet
        text={config}
        label="MCP configuration"
      />
      <p className="text-xs text-muted-foreground">
        An agent key works only against this endpoint. Sent anywhere else — the
        GraphQL API, the wiki routes — it is refused, so the agent cannot reach
        past the tools it was given.
      </p>
    </section>
  )
}

function SkillStep() {
  return (
    <section className="space-y-2">
      <StepHeading n={2} title="Teach it this app" />
      <p className="text-xs text-muted-foreground">
        A ready-made skill covering the tools, the data model and how to work
        here. Generated from this server, so it always matches the tools you
        have. Unzip it into your skills directory — per project or for every
        project:
      </p>
      <Snippet
        text={`# just this project
unzip logos-skill.zip -d .claude/skills/

# everywhere
unzip logos-skill.zip -d ~/.claude/skills/`}
        label="install commands"
      />
      <div className="flex items-center gap-1">
        <SkillDownloadButton size="sm" />
        <AllSkillsLink />
      </div>
      <p className="text-xs text-muted-foreground">
        The agent loads it on its own when the work calls for it. Logos will tell
        you here when a newer skill is available.
      </p>
    </section>
  )
}

function StepHeading({ n, title }: { n: number; title: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-[11px] font-medium text-muted-foreground">
        {n}
      </span>
      <h3 className="text-sm font-medium">{title}</h3>
    </div>
  )
}

function Snippet({ text, label }: { text: string; label: string }) {
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md border bg-muted/40 p-2.5 pr-10 font-mono text-[11px] leading-relaxed">
        {text}
      </pre>
      <Button
        size="icon-sm"
        variant="ghost"
        className="absolute top-1.5 right-1.5"
        title={`Copy ${label}`}
        onClick={() => copyToClipboard(text, label)}
      >
        <CopyIcon className="size-4" />
      </Button>
    </div>
  )
}

// Built from the live origin so the snippet is paste-ready for this deployment.
function mcpClientConfig(): string {
  return `{
  "mcpServers": {
    "logos": {
      "url": "${mcpEndpointUrl()}",
      "headers": { "Authorization": "Bearer lga_..." }
    }
  }
}`
}

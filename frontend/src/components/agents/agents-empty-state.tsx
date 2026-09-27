import { BotIcon, PlusIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  SkillDownloadButton,
  AllSkillsLink,
} from "@/components/agents/skill-download-button"
import { ConnectAgentSheet } from "@/components/agents/connect-agent-sheet"
import { useAgentStore } from "@/stores/agents"

/**
 * The empty state is the tutorial.
 *
 * Someone with no agents needs to be told what this page is for and what the
 * three steps are; someone with twelve needs the grid and nothing else. Putting
 * the explanation here rather than in a permanent block means it is prominent
 * exactly when it is wanted and gone the rest of the time — the Connect sheet in
 * the toolbar keeps the same material one click away forever.
 */
export function AgentsEmptyState() {
  const openCreateForm = useAgentStore((s) => s.openCreateForm)

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col items-center gap-6 rounded-md border border-dashed p-8 text-center sm:p-12">
      <div className="space-y-2">
        <BotIcon className="mx-auto size-7 text-muted-foreground/70" />
        <h2 className="font-heading text-base font-medium">
          Put an AI agent to work alongside you
        </h2>
        <p className="mx-auto max-w-prose text-sm text-muted-foreground">
          An agent reaches this operation through a key you mint. It acts on your
          behalf under a ceiling you set, is revocable on its own, and everything
          it does is attributed to it — not to you — on the timeline and here.
        </p>
      </div>

      <ol className="w-full space-y-3 text-left">
        <Step
          n={1}
          title="Create an agent"
          body="Name it, choose which operations it may reach, and cap what it may do. Narrowing never grants access you don't already have."
        >
          <Button size="sm" onClick={openCreateForm}>
            <PlusIcon className="size-4" />
            New agent
          </Button>
        </Step>
        <Step
          n={2}
          title="Download the skill"
          body="Generated from this server, so it always matches the tools you have. It is what stops you teaching your agent this app from scratch."
        >
          <SkillDownloadButton size="sm" />
          <AllSkillsLink />
        </Step>
        <Step
          n={3}
          title="Point the client at this server"
          body="Any MCP client works. Pass the token as a bearer credential and the agent is live."
        >
          <ConnectAgentSheet />
        </Step>
      </ol>
    </div>
  )
}

function Step({
  n,
  title,
  body,
  children,
}: {
  n: number
  title: string
  body: string
  children: React.ReactNode
}) {
  return (
    <li className="flex gap-3 rounded-md border bg-muted/20 p-3">
      <span className="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-[11px] font-medium text-muted-foreground">
        {n}
      </span>
      <div className="min-w-0 flex-1 space-y-2">
        <div className="text-sm font-medium">{title}</div>
        <p className="text-xs text-muted-foreground">{body}</p>
        <div className="flex flex-wrap items-center gap-1">{children}</div>
      </div>
    </li>
  )
}

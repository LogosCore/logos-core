import { useEffect } from "react"
import { useLocation, useNavigate } from "react-router"
import { ActivityIcon, BotIcon } from "lucide-react"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { FreshTokenBanner } from "@/components/keys/fresh-token-banner"
import { AgentsTab } from "@/components/agents/agents-tab"
import { AgentActivityTab } from "@/components/agents/agent-activity-tab"
import { useMyAgentActionSubscription } from "@/graphql/hooks/agent-actions"
import {
  AGENTS_ACTIVITY_PATH,
  AGENTS_PATH,
} from "@/hooks/use-agent-activity-params"
import { useAgentStore } from "@/stores/agents"

/**
 * Agents: the ones this operator has delegated to, and what they have been
 * doing.
 *
 * One page with two tabs rather than a dialog and a page, because it is one
 * object seen twice. Identity and ceiling on one side, the audit trail on the
 * other, and both are keyed on the same agent — an operator who spots a refused
 * call wants the scope that refused it, and an operator editing a scope wants to
 * know what the agent was doing with the old one.
 *
 * Deliberately a personal page rather than an operation one. You delegate to a
 * key, not to an engagement, and a key can reach several; it needs no scoped
 * operation to be useful, which is why it lives in the user menu beside sessions
 * rather than in the operation navigation.
 */
export function AgentsPage() {
  usePageMetadata({
    title: "Agents",
    icon: { kind: "lucide", component: BotIcon },
  })

  // Mounted at page level, not per tab: an agent working while this page is open
  // should move both the feed and its card's counts without a reload.
  useMyAgentActionSubscription()

  // The route is what selects the tab, so the two tabs stay linkable and the
  // activity filters can live in the query string. The trigger navigates rather
  // than rendering as a link: Base UI's tab must be a native button — it has no
  // public escape from that — and rendering an anchor in it drives its composite
  // list into an update loop.
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const tab = pathname.startsWith(AGENTS_ACTIVITY_PATH) ? "activity" : "agents"

  const freshToken = useAgentStore((s) => s.freshToken)
  const setFreshToken = useAgentStore((s) => s.setFreshToken)
  const reset = useAgentStore((s) => s.reset)

  // Leaving the page drops the token. Keeping it would let a secret reappear
  // long after the operator moved on — and the banner's whole contract is that
  // it is shown once, now.
  useEffect(() => reset, [reset])

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      {/* Above the tabs rather than inside a card: it is shown exactly once, and
          a banner that can be scrolled out of a grid is a banner that can be
          missed. */}
      {freshToken && (
        <FreshTokenBanner
          token={freshToken}
          onDismiss={() => setFreshToken(null)}
        />
      )}

      <Tabs
        value={tab}
        onValueChange={(value) =>
          navigate(value === "activity" ? AGENTS_ACTIVITY_PATH : AGENTS_PATH)
        }
        className="flex min-h-0 flex-1 flex-col gap-2"
      >
        <TabsList>
          <TabsTrigger value="agents">
            <BotIcon className="size-3.5" />
            Agents
          </TabsTrigger>
          <TabsTrigger value="activity">
            <ActivityIcon className="size-3.5" />
            Activity
          </TabsTrigger>
        </TabsList>

        <TabsContent value="agents" className="flex min-h-0 flex-col">
          <AgentsTab />
        </TabsContent>
        <TabsContent value="activity" className="flex min-h-0 flex-col">
          <AgentActivityTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}

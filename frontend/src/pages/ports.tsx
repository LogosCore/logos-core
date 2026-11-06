import { useState } from "react"
import { EthernetPortIcon, SearchXIcon } from "lucide-react"
import { SearchInput } from "@/components/ui/search-input"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { PortsTable } from "@/components/ports/ports-table"
import { useOperationalPorts, useOperationalPortsSubscription } from "@/graphql/hooks/operational-ports"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"

/**
 * The scoped operation's operational ports in use (docs:
 * contracts/minion-messages/tunnels.md, Operational ports): core's ports
 * its tunnel tasks' SOCKS proxies listen on, each until its task ends.
 */
export function PortsPage() {
  const scopedOperation = useScopedOperation()
  usePageMetadata({
    title: scopedOperation ? `Ports · ${scopedOperation.name}` : "Ports",
    icon: { kind: "lucide", component: EthernetPortIcon },
  })
  if (!scopedOperation) return <GettingStartedPanel />
  return <PortsPageInner operationId={scopedOperation.id} />
}

function PortsPageInner({ operationId }: { operationId: string }) {
  // The server searches (see the operationalPorts query).
  const [search, setSearch] = useState("")
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useOperationalPorts({ operationId, search: search.trim() })
  // Live: ports opening, their streams, ports closing.
  useOperationalPortsSubscription(operationId)
  const ports = useConnectionNodes(data, (p) => p.operationalPorts)

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput value={search} onValueChange={setSearch} placeholder="Search the port, #session, who opened it..." />
      </div>
      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <PortsTable
          ports={ports}
          isLoading={isLoading}
          isFetchingNextPage={isFetchingNextPage}
          hasNextPage={hasNextPage}
          fetchNextPage={() => void fetchNextPage()}
          emptyState={
            search.trim() !== "" ? (
              <>
                <SearchXIcon className="size-8 opacity-50" />
                <p className="text-sm">No port matches.</p>
              </>
            ) : (
              <>
                <EthernetPortIcon className="size-8 opacity-50" />
                <p className="text-sm">No port in use in this operation.</p>
                <p className="max-w-md text-center text-xs">
                  A SOCKS proxy task on a session takes one of core&apos;s operational ports while it runs: what a tool
                  reaches through it, the session&apos;s minion connects to from its host.
                </p>
              </>
            )
          }
        />
      )}
    </div>
  )
}

import { useState } from "react"
import { EthernetPortIcon, NetworkIcon, SearchXIcon, ServerIcon } from "lucide-react"
import { SearchInput } from "@/components/ui/search-input"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ListenersTable } from "@/components/ports/listeners-table"
import { PortsTable } from "@/components/ports/ports-table"
import { VPNTunnelsTable } from "@/components/ports/vpn-tunnels-table"
import { useMinionListeners, useMinionListenersSubscription } from "@/graphql/hooks/minion-listeners"
import { useOperationalPorts, useOperationalPortsSubscription } from "@/graphql/hooks/operational-ports"
import { useVPNTunnels, useVPNTunnelsSubscription } from "@/graphql/hooks/vpn-tunnels"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"

/**
 * The ports in use of the scoped operation, or with none scoped of every
 * operation the user is a member of, each row saying its operation (as
 * Findings without an operation) (docs:
 * contracts/minion-messages/tunnels.md): core's operational ports, which
 * its tunnel tasks' SOCKS proxies and port forwards listen on, and the
 * ports its reverse port forwards' minions listen on, each until its task
 * ends; and its VPN tunnels (docs: contracts/vpn-front), which take no
 * port: their routes and machines.
 */
export function PortsPage() {
  const scopedOperation = useScopedOperation()
  usePageMetadata({
    title: scopedOperation ? `Ports · ${scopedOperation.name}` : "Ports",
    icon: { kind: "lucide", component: EthernetPortIcon },
  })
  // A list each scope: its search and tab start afresh with it.
  return <PortsPageInner key={scopedOperation?.id ?? "all"} operationId={scopedOperation?.id ?? null} />
}

type Tab = "core" | "minions" | "vpns"

/** operationId null: every operation the user is a member of. */
function PortsPageInner({ operationId }: { operationId: string | null }) {
  const all = operationId === null
  // How an empty list names where it looked.
  const where = all ? "in your operations" : "in this operation"
  // The server searches both lists (see the operationalPorts and
  // minionListeners queries).
  const [search, setSearch] = useState("")
  const [tab, setTab] = useState<Tab>("core")
  const q = search.trim()
  const portsQ = useOperationalPorts({ operationId, search: q })
  const listenersQ = useMinionListeners({ operationId, search: q })
  const vpnsQ = useVPNTunnels({ operationId, search: q })
  // Live: ports, listeners and VPNs opening, their streams, closing.
  useOperationalPortsSubscription(operationId)
  useMinionListenersSubscription(operationId)
  useVPNTunnelsSubscription(operationId)
  const ports = useConnectionNodes(portsQ.data, (p) => p.operationalPorts)
  const listeners = useConnectionNodes(listenersQ.data, (p) => p.minionListeners)
  const portCount = portsQ.data?.pages[0]?.operationalPorts.totalCount
  const listenerCount = listenersQ.data?.pages[0]?.minionListeners.totalCount
  const vpns = useConnectionNodes(vpnsQ.data, (p) => p.vpnTunnels)
  const vpnCount = vpnsQ.data?.pages[0]?.vpnTunnels.totalCount
  const noMatch = (
    <>
      <SearchXIcon className="size-8 opacity-50" />
      <p className="text-sm">No port matches.</p>
    </>
  )

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="flex min-h-0 flex-1 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <TabsList>
            <TabsTrigger value="core">
              <EthernetPortIcon className="size-3.5" />
              Core&apos;s ports
              {portCount != null && <span className="text-muted-foreground tabular-nums">{portCount}</span>}
            </TabsTrigger>
            <TabsTrigger value="minions">
              <ServerIcon className="size-3.5" />
              Minions&apos; ports
              {listenerCount != null && <span className="text-muted-foreground tabular-nums">{listenerCount}</span>}
            </TabsTrigger>
            <TabsTrigger value="vpns">
              <NetworkIcon className="size-3.5" />
              VPNs
              {vpnCount != null && <span className="text-muted-foreground tabular-nums">{vpnCount}</span>}
            </TabsTrigger>
          </TabsList>
          <SearchInput value={search} onValueChange={setSearch} placeholder="Search the port, #session, who opened it, the destination..." />
        </div>

        <TabsContent value="core" className="flex min-h-0 flex-col">
          {portsQ.error ? (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{portsQ.error.message}</div>
          ) : (
            <PortsTable
              ports={ports}
              isLoading={portsQ.isLoading}
              isFetchingNextPage={portsQ.isFetchingNextPage}
              hasNextPage={portsQ.hasNextPage}
              fetchNextPage={() => void portsQ.fetchNextPage()}
              showOperation={all}
              emptyState={
                q !== "" ? (
                  noMatch
                ) : (
                  <>
                    <EthernetPortIcon className="size-8 opacity-50" />
                    <p className="text-sm">No port of core&apos;s in use {where}.</p>
                    <p className="max-w-md text-center text-xs">
                      A SOCKS proxy or a port forward task on a session takes one of core&apos;s operational ports while it
                      runs: what a tool reaches through it, the session&apos;s minion connects to from its host.
                    </p>
                  </>
                )
              }
            />
          )}
        </TabsContent>
        <TabsContent value="minions" className="flex min-h-0 flex-col">
          {listenersQ.error ? (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{listenersQ.error.message}</div>
          ) : (
            <ListenersTable
              listeners={listeners}
              isLoading={listenersQ.isLoading}
              isFetchingNextPage={listenersQ.isFetchingNextPage}
              hasNextPage={listenersQ.hasNextPage}
              fetchNextPage={() => void listenersQ.fetchNextPage()}
              showOperation={all}
              emptyState={
                q !== "" ? (
                  noMatch
                ) : (
                  <>
                    <ServerIcon className="size-8 opacity-50" />
                    <p className="text-sm">No minion listens {all ? "for your operations" : "for this operation"}.</p>
                    <p className="max-w-md text-center text-xs">
                      A reverse port forward task on a session has its minion listen on its host while it runs: core
                      connects each connection that comes there to the task&apos;s destination, from core&apos;s network.
                    </p>
                  </>
                )
              }
            />
          )}
        </TabsContent>
        <TabsContent value="vpns" className="flex min-h-0 flex-col">
          {vpnsQ.error ? (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{vpnsQ.error.message}</div>
          ) : (
            <VPNTunnelsTable
              tunnels={vpns}
              isLoading={vpnsQ.isLoading}
              isFetchingNextPage={vpnsQ.isFetchingNextPage}
              hasNextPage={vpnsQ.hasNextPage}
              fetchNextPage={() => void vpnsQ.fetchNextPage()}
              showOperation={all}
              emptyState={
                q !== "" ? (
                  noMatch
                ) : (
                  <>
                    <NetworkIcon className="size-8 opacity-50" />
                    <p className="text-sm">No VPN {where}.</p>
                    <p className="max-w-md text-center text-xs">
                      A tunnel task whose front is VPN routes operators&apos; machines into its networks while it runs:
                      each machine runs logos-vpn, and what it reaches there, the session&apos;s minion connects to from
                      its host.
                    </p>
                  </>
                )
              }
            />
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}

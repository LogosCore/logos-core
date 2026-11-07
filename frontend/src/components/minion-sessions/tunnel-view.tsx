import { useState } from "react";
import {
  CopyIcon,
  EthernetPortIcon,
  LinkIcon,
  SearchXIcon,
  TerminalIcon,
  WaypointsIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { InfoCard, LabelRow } from "@/components/ui/info-card";
import { SearchInput } from "@/components/ui/search-input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  VirtualizedDataTable,
  dataTableRowClass,
} from "@/components/ui/virtualized-data-table";
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql";
import {
  useMinionTunnelStreams,
  type TunnelStreamsFilter,
} from "@/graphql/hooks/minion-tasks";
import { copyToClipboard } from "@/lib/copy-to-clipboard";
import { formatDateTime } from "@/lib/date-format";
import { formatBytes } from "@/lib/format-bytes";
import { cn } from "@/lib/utils";
import { FINISHED } from "./task-actions";
import {
  PORT_FORWARD,
  REVERSE_PORT_FORWARD,
  frontLabel,
  hostPort,
  listenAddress,
  proxyState,
  proxyURL,
  proxychainsLine,
  reverseState,
  stopIdleOf,
  streamState,
  tunnelVersion,
  type Tone,
  type Tunnel,
  type TunnelStream,
} from "./tunnel-values";

// A tunnel task's proxy (docs: contracts/minion-messages/tunnels.md, What
// core keeps and shows): core's SOCKS on one of its operational ports while
// the task runs. The strip says where it is and copies it in the forms
// tools take; the cards, what it is and what went through it; the table,
// each connection a tool made through it, live, the latest first, searched
// and filtered by the server, while the task runs: core keeps no stream
// once it ended (its numbers stay). A reverse port forward's says the same from
// the minion's side: where its minion listens, the destination core
// connects each connection there to, and who connected.

type Task = MinionTaskFieldsFragment;

const TONE: Record<Tone, string> = {
  pending: "text-amber-600 dark:text-amber-400",
  good: "text-emerald-600 dark:text-emerald-400",
  muted: "text-muted-foreground",
  bad: "text-red-600 dark:text-red-400",
};

const GRID_COLS =
  "grid-cols-[3.5rem_minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,1.4fr)_minmax(0,1fr)_5.5rem_5.5rem]";

export function TunnelView({
  task: t,
  tunnel,
}: {
  task: Task;
  tunnel: Tunnel;
}) {
  // A reverse port forward listens on the minion's host: no port of core's.
  const reverse = tunnel.purpose === REVERSE_PORT_FORWARD;
  // Core's address for its ports, else the host this page came from; a
  // reverse port forward's, where its minion listens.
  const host = tunnel.address ?? window.location.hostname;
  const address = reverse ? listenAddress(tunnel) : hostPort(host, tunnel.port);
  const state = reverse
    ? reverseState(tunnel, t.state)
    : proxyState(tunnel, t.state);
  // A forward (either way): every connection goes to its destination; a
  // SOCKS proxy: where each tool says.
  const forward = tunnel.purpose === PORT_FORWARD || reverse;
  const [filter, setFilter] = useState<TunnelStreamsFilter>({
    search: "",
    state: "",
  });
  // Core keeps the streams while the task runs (every open one, the latest
  // ended ones): once it ended, or is to, none.
  const keeps = !FINISHED.has(t.state) && !t.cancelRequestedAt;
  const streamsQ = useMinionTunnelStreams(
    t.id,
    filter,
    tunnelVersion(tunnel),
    keeps,
  );
  const streams = keeps
    ? (streamsQ.data?.pages.flatMap((p) => p.minionTunnelStreams.streams) ?? [])
    : [];
  const filtered = filter.search !== "" || filter.state !== "";
  // Core stops the task once no stream has been open this long (its
  // stop_idle; none: never).
  const stopIdle = stopIdleOf(t.args);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-8 flex-wrap items-center gap-x-2 gap-y-1 border-b px-2 py-1">
        <span className="flex min-w-0 flex-1 items-baseline gap-1.5 px-1">
          <EthernetPortIcon
            className="size-3.5 shrink-0 self-center text-muted-foreground"
            aria-hidden
          />
          <span className="truncate text-xs font-medium">
            {frontLabel(tunnel.purpose)}
          </span>
          {tunnel.udp && (
            <span className="shrink-0 text-xs text-muted-foreground">UDP</span>
          )}
          <span className="truncate font-mono text-xs">{address}</span>
          {forward && tunnel.to && (
            <span className="truncate font-mono text-xs text-muted-foreground">
              → {tunnel.to}
            </span>
          )}
          <span
            className={cn("shrink-0 text-xs", TONE[state.tone])}
            title={state.detail ?? undefined}
          >
            {state.label}
          </span>
        </span>
        {tunnel.listening && (
          <div className="ml-auto flex items-center gap-1">
            <Button
              variant="ghost"
              size="xs"
              title={`Copy ${address}`}
              onClick={() => void copyToClipboard(address, "address")}
            >
              <CopyIcon />
              Address
            </Button>
            {!forward && (
              <>
                <Button
                  variant="ghost"
                  size="xs"
                  title={`Copy ${proxyURL(host, tunnel.port)}: for curl -x, ALL_PROXY or a browser (names resolve on the minion's host)`}
                  onClick={() =>
                    void copyToClipboard(
                      proxyURL(host, tunnel.port),
                      "proxy URL",
                    )
                  }
                >
                  <LinkIcon />
                  URL
                </Button>
                <Button
                  variant="ghost"
                  size="xs"
                  title={`Copy "${proxychainsLine(host, tunnel.port)}": a line of proxychains' [ProxyList]`}
                  onClick={() =>
                    void copyToClipboard(
                      proxychainsLine(host, tunnel.port),
                      "proxychains line",
                    )
                  }
                >
                  <TerminalIcon />
                  proxychains
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3 @container">
        <div className="grid shrink-0 items-start gap-3 @3xl:grid-cols-2">
          <InfoCard labelWidth="w-28">
            <LabelRow label={reverse ? "Listens on" : "Address"}>
              <span className="font-mono text-xs break-all">{address}</span>
              {reverse ? (
                <span className="text-muted-foreground">
                  {" "}
                  · the minion&apos;s host{tunnel.udp && ", UDP"}
                  {!tunnel.bound && ", as the task asks"}
                </span>
              ) : (
                !tunnel.address && (
                  <span className="text-muted-foreground">
                    {" "}
                    · this page's host
                  </span>
                )
              )}
            </LabelRow>
            {forward ? (
              <LabelRow label="Forwards to">
                <span className="font-mono text-xs break-all">
                  {tunnel.to ?? "?"}
                </span>
                <span className="text-muted-foreground">
                  {reverse
                    ? tunnel.udp
                      ? " · from core's network, a socket for each address datagrams come from"
                      : " · from core's network, each connection"
                    : " · from the minion's host, each connection"}
                </span>
              </LabelRow>
            ) : (
              <LabelRow label="Speaks">
                SOCKS5 (connect, UDP associate), SOCKS4 and 4a (connect); no
                credentials
              </LabelRow>
            )}
            <LabelRow label="State">
              <span className={TONE[state.tone]}>{state.label}</span>
              {state.detail && (
                <span className="text-muted-foreground">: {state.detail}</span>
              )}
            </LabelRow>
            <LabelRow label="Stops when idle">
              {stopIdle ? (
                <>
                  after {stopIdle} with no stream open
                  {tunnel.stopsAt ? (
                    <span className="text-muted-foreground">
                      {" "}
                      · at {formatDateTime(tunnel.stopsAt)} unless one opens
                    </span>
                  ) : (
                    tunnel.open > 0 && (
                      <span className="text-muted-foreground">
                        {" "}
                        · not while a stream is open
                      </span>
                    )
                  )}
                </>
              ) : (
                <span className="text-muted-foreground">never</span>
              )}
            </LabelRow>
          </InfoCard>
          <InfoCard labelWidth="w-28">
            <LabelRow label="Streams">
              <span className="tabular-nums">{tunnel.streams}</span>
              {tunnel.listening && (
                <span className="text-muted-foreground tabular-nums">
                  {" "}
                  · {tunnel.open} open now
                </span>
              )}
              {keeps && tunnel.streams > tunnel.kept && (
                <span className="text-muted-foreground tabular-nums">
                  {" "}
                  · the latest {tunnel.kept} listed
                </span>
              )}
            </LabelRow>
            <LabelRow label="Sent">
              {/* Core's up is what it read of its end: the tools', or a reverse forward's destination's. */}
              <span className="tabular-nums">
                {formatBytes(reverse ? tunnel.down : tunnel.up)}
              </span>
              <span className="text-muted-foreground">
                {reverse ? " by who connected" : " from the tools"}
              </span>
            </LabelRow>
            <LabelRow label="Received">
              <span className="tabular-nums">
                {formatBytes(reverse ? tunnel.up : tunnel.down)}
              </span>
              <span className="text-muted-foreground"> back</span>
            </LabelRow>
          </InfoCard>
        </div>

        {keeps && (
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <SearchInput
              value={filter.search}
              onValueChange={(search) => setFilter((f) => ({ ...f, search }))}
              placeholder={
                reverse
                  ? "Search who connected, or the destination..."
                  : "Search the tool or the destination..."
              }
            />
            <Select
              value={filter.state || "all"}
              onValueChange={(v) =>
                setFilter((f) => ({
                  ...f,
                  state: v === "all" ? "" : (v as "OPEN" | "ENDED"),
                }))
              }
            >
              <SelectTrigger className="w-36" aria-label="State">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All streams</SelectItem>
                <SelectItem value="OPEN">Open now</SelectItem>
                <SelectItem value="ENDED">Ended</SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}

        <div className="flex min-h-72 flex-1 flex-col">
          {streamsQ.error ? (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">
              {streamsQ.error.message}
            </div>
          ) : (
            <VirtualizedDataTable
              items={streams}
              isLoading={keeps && streamsQ.isLoading}
              isFetchingNextPage={keeps && streamsQ.isFetchingNextPage}
              hasNextPage={keeps && streamsQ.hasNextPage}
              fetchNextPage={() => void streamsQ.fetchNextPage()}
              gridCols={GRID_COLS}
              entityNoun="streams"
              header={
                <>
                  <div>#</div>
                  <div>{reverse ? "From" : "Tool"}</div>
                  <div>Destination</div>
                  <div>State</div>
                  <div>Opened</div>
                  <div className="text-right">Sent</div>
                  <div className="text-right">Received</div>
                </>
              }
              emptyState={
                !keeps ? (
                  <>
                    <WaypointsIcon className="size-8 opacity-50" />
                    <p className="text-sm">
                      No streams are kept once the task ends.
                    </p>
                    <p className="max-w-md text-center text-xs">
                      While it runs, core lists every open stream and the latest{" "}
                      {tunnel.kept} that ended; what stays is the numbers above.
                    </p>
                  </>
                ) : filtered ? (
                  <>
                    <SearchXIcon className="size-8 opacity-50" />
                    <p className="text-sm">No stream matches.</p>
                  </>
                ) : (
                  <>
                    <WaypointsIcon className="size-8 opacity-50" />
                    <p className="text-sm">No stream yet.</p>
                    <p className="max-w-md text-center text-xs">
                      {reverse && tunnel.udp
                        ? `Each address that sends datagrams to ${address} on the minion's host is one: core sends them on to ${tunnel.to ?? "the destination"} from its own network.`
                        : reverse
                          ? `Each connection to ${address} on the minion's host is one: core connects it to ${tunnel.to ?? "the destination"} from its own network.`
                          : !tunnel.listening
                            ? "No tool connected through it."
                            : forward
                              ? `Each connection to ${address} is one: the minion connects to ${tunnel.to ?? "the destination"} from its host.`
                              : `Each connection a tool makes through ${address} is one: the minion connects to its destination from its host.`}
                    </p>
                  </>
                )
              }
              renderRow={(s) => <StreamRow s={s} reverse={reverse} />}
            />
          )}
        </div>
      </div>
    </div>
  );
}

function StreamRow({ s, reverse }: { s: TunnelStream; reverse: boolean }) {
  const state = streamState(s, reverse);
  // A UDP association's datagrams each name their own destination; a UDP
  // reverse forward's stream has one, core's.
  const destination =
    s.udp && !reverse ? "UDP: each datagram its own" : hostPort(s.host, s.port);
  return (
    <div className={dataTableRowClass(GRID_COLS)}>
      <span className="font-mono text-xs text-muted-foreground tabular-nums">
        {s.stream}
      </span>
      <span className="truncate font-mono text-xs" title={s.client}>
        {s.client}
      </span>
      <span
        className={cn("truncate text-xs", (!s.udp || reverse) && "font-mono")}
        title={destination}
      >
        {destination}
      </span>
      <span className="flex min-w-0 flex-col">
        <span className={cn("text-xs", TONE[state.tone])}>{state.label}</span>
        {state.detail && (
          <span
            className="truncate text-xs text-muted-foreground"
            title={state.detail}
          >
            {state.detail}
          </span>
        )}
      </span>
      <span className="flex min-w-0 flex-col text-xs tabular-nums">
        <span className="truncate">{formatDateTime(s.openedAt)}</span>
        {s.endedAt && (
          <span className="truncate text-muted-foreground">
            to {formatDateTime(s.endedAt)}
          </span>
        )}
      </span>
      {/* Sent by who started it: a tool's (core's up), or who connected to a reverse forward's minion (core's down). */}
      <span className="text-right text-xs tabular-nums">
        {formatBytes(reverse ? s.down : s.up)}
      </span>
      <span className="text-right text-xs tabular-nums">
        {formatBytes(reverse ? s.up : s.down)}
      </span>
    </div>
  );
}

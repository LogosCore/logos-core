import { useState } from "react"
import { CopyIcon, DownloadIcon, LaptopIcon, MonitorSmartphoneIcon, PencilIcon, RefreshCwIcon, UnplugIcon } from "lucide-react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import {
  useCreateMinionVPNKey,
  useDisconnectMinionVPNClient,
  useSetMinionTunnelRoutes,
} from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import {
  VPN_PLATFORMS,
  clientSystem,
  coreOrigin,
  guessPlatform,
  parseRoutes,
  vpnCommand,
  type VPNClient,
} from "./tunnel-values"

// A VPN tunnel's own parts (docs: contracts/vpn-front): connecting a
// machine (a key of the operator's, the one command to run there, and the
// client for its system), its routes changed live, and the machines
// connected.

const API_URL = import.meta.env.VITE_API_URL as string

/** The client for a system, as core serves it (the session cookie authenticates it). */
function clientURL(platform: string): string {
  return `${API_URL}/vpn/client/${platform}`
}

/** Connect a machine: a new key each time it opens, and the command that uses it. */
export function ConnectMachine({ taskId }: { taskId: string }) {
  const [open, setOpen] = useState(false)
  const [platform, setPlatform] = useState(() => guessPlatform(navigator.userAgent))
  const create = useCreateMinionVPNKey()
  const key = create.data?.createMinionVPNKey
  const origin = coreOrigin(API_URL, window.location.origin)
  const command = key ? vpnCommand(origin, key.key, platform) : ""
  const windows = platform.startsWith("windows-")
  const newKey = () => create.mutate({ taskId }, { onError: (err) => toast.error(errorMessage(err, "Could not make a key")) })
  return (
    <>
      <Button
        variant="ghost"
        size="xs"
        title="Connect a machine: the one command to run there"
        onClick={() => {
          setOpen(true)
          newKey()
        }}
      >
        <LaptopIcon />
        Connect a machine
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Connect a machine</DialogTitle>
            <DialogDescription>
              Run this on the machine to route into the minion&apos;s networks. While it runs, every tool there reaches
              the tunnel&apos;s routes; Ctrl-C disconnects it.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <Select value={platform} onValueChange={(v) => v && setPlatform(v)}>
                <SelectTrigger className="w-56" aria-label="The machine's system">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {VPN_PLATFORMS.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                size="sm"
                nativeButton={false}
                render={<a href={clientURL(platform)} download={windows ? "logos-vpn.exe" : "logos-vpn"} />}
              >
                <DownloadIcon />
                Download logos-vpn
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {windows
                ? "Once: download it. Then, in an elevated PowerShell where it is:"
                : "Once: download it, make it runnable and put it on the PATH (chmod +x logos-vpn; sudo mv logos-vpn /usr/local/bin/). Then, on the machine:"}
            </p>
            <div className="flex items-start gap-2">
              <pre className="min-h-9 flex-1 overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap">
                {create.isPending ? "Making a key…" : command || "No key"}
              </pre>
              <Button
                variant="outline"
                size="icon-sm"
                title="Copy the command"
                aria-label="Copy the command"
                disabled={!command}
                onClick={() => void copyToClipboard(command, "command")}
              >
                <CopyIcon />
              </Button>
            </div>
            <p className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
              {key && <>The key is yours, for this tunnel: unused, it lapses at {formatDateTime(key.lapsesAt)}; used, it is that machine&apos;s until it disconnects.</>}
              <Button variant="ghost" size="xs" onClick={newKey} disabled={create.isPending}>
                <RefreshCwIcon />
                New key
              </Button>
            </p>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

/** Changes a running VPN's routes: its machines take them at once. */
export function ChangeRoutes({ taskId, routes }: { taskId: string; routes: string[] }) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState("")
  const set = useSetMinionTunnelRoutes()
  const parsed = parseRoutes(text)
  const submit = () => {
    if (!parsed.ok) return
    set.mutate(
      { taskId, routes: parsed.value },
      {
        onSuccess: () => {
          setOpen(false)
          toast.success("Routes changed: the machines take them now")
        },
        onError: (err) => toast.error(errorMessage(err, "Could not change them")),
      },
    )
  }
  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) setText(routes.join("\n"))
      }}
    >
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="ml-1 align-middle"
            title="Change the routes"
            aria-label="Change the routes"
          >
            <PencilIcon />
          </Button>
        }
      />
      <PopoverContent align="start" className="w-80">
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <label htmlFor={`routes-${taskId}`} className="text-sm font-medium">
            Networks, one a line
          </label>
          <Textarea
            id={`routes-${taskId}`}
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={5}
            className="font-mono text-xs"
            aria-invalid={!parsed.ok}
            autoFocus
          />
          <p className={!parsed.ok ? "text-xs text-red-600 dark:text-red-400" : "text-xs text-muted-foreground"}>
            {!parsed.ok ? parsed.error : "As CIDRs (10.0.0.0/24, fd00:1::/64), 64 at most, no default route."}
          </p>
          <Button type="submit" size="sm" className="self-end" disabled={!parsed.ok || set.isPending}>
            Set
          </Button>
        </form>
      </PopoverContent>
    </Popover>
  )
}

/** The machines a VPN has: connected, or kept a while since their link dropped. */
export function VPNClients({ taskId, clients, running }: { taskId: string; clients: VPNClient[]; running: boolean }) {
  const disconnect = useDisconnectMinionVPNClient()
  if (clients.length === 0) {
    return (
      <div className="flex shrink-0 items-center gap-2 rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
        <MonitorSmartphoneIcon className="size-4 shrink-0" aria-hidden />
        {running
          ? "No machine connected: Connect a machine gives the command to run on one."
          : "No machine: the task ended."}
      </div>
    )
  }
  return (
    <div className="flex shrink-0 flex-col divide-y rounded-md border">
      {clients.map((c) => (
        <div key={c.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-1.5 text-xs">
          <span className="flex min-w-0 items-baseline gap-1.5">
            <LaptopIcon className="size-3.5 shrink-0 self-center text-muted-foreground" aria-hidden />
            <span className="truncate font-mono">{c.name.replace(/^vpn: /, "")}</span>
          </span>
          <span className={c.connected ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"}>
            {c.connected ? "connected" : "link down: kept a while for another"}
          </span>
          <span className="text-muted-foreground">{clientSystem(c)}</span>
          <span className="font-mono text-muted-foreground">from {c.from}</span>
          <span className="text-muted-foreground tabular-nums">since {formatDateTime(c.since)}</span>
          <span className="tabular-nums">
            {c.open} open · {c.streams} opened · {formatBytes(c.up)} sent · {formatBytes(c.down)} back
          </span>
          {running && (
            <Button
              variant="ghost"
              size="xs"
              className="ml-auto"
              title={`Disconnect ${c.name}: its routes go from its machine`}
              disabled={disconnect.isPending}
              onClick={() =>
                disconnect.mutate(
                  { taskId, client: c.id },
                  {
                    onSuccess: () => toast.success(`Disconnected ${c.name.replace(/^vpn: /, "")}`),
                    onError: (err) => toast.error(errorMessage(err, "Could not disconnect it")),
                  },
                )
              }
            >
              <UnplugIcon />
              Disconnect
            </Button>
          )}
        </div>
      ))}
    </div>
  )
}

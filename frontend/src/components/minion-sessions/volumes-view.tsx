import { FolderTreeIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { VolumeKindIcon, VolumeUsage } from "./volume-parts"
import { KIND_LABELS, type Volumes } from "./volumes"

// A volumes task's view: the host's volumes, as the task listed them. A
// drive's root opens in the machine's file browser, where the volumes are
// its roots (and mount points marked).

export function VolumesView({
  volumes: { volumes },
  onOpenInFileBrowser,
}: {
  volumes: Volumes
  /** Opens a volume's path in the machine's file browser. */
  onOpenInFileBrowser?: (path: string) => void
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b px-3 py-1 text-xs text-muted-foreground">
        {volumes.length} {volumes.length === 1 ? "volume" : "volumes"}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {volumes.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">The host said it has none.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="px-3 py-1 font-medium">Volume</th>
                <th className="px-3 py-1 font-medium">Kind</th>
                <th className="px-3 py-1 font-medium">File system</th>
                <th className="px-3 py-1 font-medium">Space</th>
                <th className="px-3 py-1 font-medium">Device</th>
                {onOpenInFileBrowser && <th className="w-0 px-1 py-1" />}
              </tr>
            </thead>
            <tbody>
              {volumes.map((v) => (
                <tr key={v.path} className="border-b last:border-b-0 hover:bg-muted/40">
                  <td className="max-w-0 min-w-48 px-3 py-1">
                    <span className="inline-flex max-w-full items-baseline gap-1.5">
                      <VolumeKindIcon kind={v.kind} className="self-center text-muted-foreground" />
                      <span className="truncate font-mono text-xs" title={v.path}>
                        {v.path}
                      </span>
                      {v.label && (
                        <span className="truncate text-muted-foreground" title={v.label}>
                          {v.label}
                        </span>
                      )}
                    </span>
                  </td>
                  <td className="px-3 py-1 whitespace-nowrap text-muted-foreground">
                    {KIND_LABELS[v.kind]}
                    {v.readOnly && <span className="ml-1.5 rounded bg-muted px-1 text-xs">read-only</span>}
                  </td>
                  <td className={cn("px-3 py-1 whitespace-nowrap", !v.fs && "text-muted-foreground")}>{v.fs ?? ""}</td>
                  <td className="px-3 py-1">
                    <VolumeUsage volume={v} />
                  </td>
                  <td className="max-w-0 min-w-24 truncate px-3 py-1 font-mono text-xs text-muted-foreground" title={v.device}>
                    {v.device ?? ""}
                  </td>
                  {onOpenInFileBrowser && (
                    <td className="px-1 py-0.5">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        title={v.noMedia ? "Nothing in the drive to browse" : `Open ${v.path} in the File Browser`}
                        aria-label={`Open ${v.path} in the File Browser`}
                        disabled={v.noMedia}
                        onClick={() => onOpenInFileBrowser(v.path)}
                      >
                        <FolderTreeIcon />
                      </Button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

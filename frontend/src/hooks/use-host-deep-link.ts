import { useEffect, useRef } from "react"
import { useSearchParams } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { HostDocument } from "@/graphql/gql/graphql"
import { hostKeys } from "@/graphql/hooks/hosts"
import { graphqlClient } from "@/lib/graphql-client"
import { useHostStore } from "@/stores/hosts"
import { useFindingsStore } from "@/stores/findings"

// `?host=<id>` on the Findings page opens that host's dialog on the hosts
// tab; closing the dialog strips the param. Hosts had no address of their
// own, as credentials, hashes and tasks do; with one, a link to a host can be
// shared, and pasted onto a drawing as its card (drawing-record-cards.ts).
//
// One way, unlike the credential and hash links: the host dialog is an edit
// form opened from many places, and writing the URL on every open would make
// the table's row clicks churn history for no one's benefit.
const HOST_PARAM = "host"

/** `enabled` is false where hosts cannot be shown (the global view). */
export function useHostDeepLink(enabled: boolean) {
  const [searchParams, setSearchParams] = useSearchParams()
  const urlHostId = searchParams.get(HOST_PARAM)
  const queryClient = useQueryClient()
  const formDialogOpen = useHostStore((s) => s.formDialogOpen)
  const openEditDialog = useHostStore((s) => s.openEditDialog)
  const setActiveTab = useFindingsStore((s) => s.setActiveTab)

  // URL → dialog, on the URL's change to an id.
  const prevUrlIdRef = useRef<string | null>(null)
  useEffect(() => {
    const prev = prevUrlIdRef.current
    prevUrlIdRef.current = urlHostId
    if (!enabled || !urlHostId || prev === urlHostId) return
    let cancelled = false
    setActiveTab("hosts")
    queryClient
      .fetchQuery({
        queryKey: hostKeys.detail(urlHostId),
        queryFn: () => graphqlClient(HostDocument, { id: urlHostId }),
      })
      .then(({ host }) => {
        if (cancelled) return
        if (host) openEditDialog(host)
        else toast.error("Host not found — it may have been deleted")
      })
      .catch(() => {
        if (!cancelled) toast.error("Could not open that host")
      })
    return () => {
      cancelled = true
    }
  }, [enabled, urlHostId, queryClient, openEditDialog, setActiveTab])

  // Closing the dialog drops the param, so a reload does not reopen it.
  const wasOpenRef = useRef(false)
  useEffect(() => {
    const wasOpen = wasOpenRef.current
    wasOpenRef.current = formDialogOpen
    if (wasOpen && !formDialogOpen && urlHostId) {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.delete(HOST_PARAM)
          return next
        },
        { replace: true },
      )
    }
  }, [formDialogOpen, urlHostId, setSearchParams])
}

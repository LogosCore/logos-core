import { toast } from "sonner"
import { useUnblockModule } from "@/graphql/hooks/modules"
import { errorMessage } from "./management/api-errors"

/** Unblock an instance at once (no confirmation: it only lets it back in), with a toast either way. */
export function useUnblockWithToast() {
  const m = useUnblockModule()
  return {
    pending: m.isPending,
    unblock: (instance: string) =>
      m.mutate(
        { instance },
        {
          onSuccess: () => toast.success(`Unblocked ${instance}`),
          onError: (err) => toast.error("Failed to unblock", { description: errorMessage(err, "Request failed") }),
        },
      ),
  }
}

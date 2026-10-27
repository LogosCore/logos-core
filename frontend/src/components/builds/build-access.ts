import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { useMyOperationRole } from "@/graphql/hooks/operations"

/**
 * Whether the caller may build in the operation, and change its build files:
 * its operators and admins, and app admins (the server allows them in any
 * operation; OPERATION_DELETE is what only they hold).
 */
export function useCanBuild(operationId: string): { canWrite: boolean; loading: boolean } {
  const hasPermission = useAuthStore((s) => s.hasPermission)
  const { data, isLoading } = useMyOperationRole(operationId)
  const role = data?.myOperationRole ?? null
  return {
    canWrite: role === "ADMIN" || role === "OPERATOR" || hasPermission(Permissions.OPERATION_DELETE),
    loading: isLoading,
  }
}

/**
 * Why an action cannot be run now, or undefined when it can: the caller's
 * permissions first, then the instance, then the action's own conditions
 * (available_when), which core evaluates against the latest status values.
 */
export function actionBlocker(
  action: { risk: string; available: boolean; unavailableReason?: string | null },
  ctx: { canInvoke: boolean; canInvokeHigh: boolean; registered: boolean },
): string | undefined {
  if (!ctx.canInvoke) return "Requires module:action:invoke"
  if (action.risk === "high" && !ctx.canInvokeHigh) {
    return "High-risk actions require module:action:invoke:high"
  }
  if (!ctx.registered) return "The instance is not registered"
  if (!action.available) return action.unavailableReason ?? "Not available now"
  return undefined
}

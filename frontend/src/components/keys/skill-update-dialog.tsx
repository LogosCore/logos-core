import { lazy, Suspense, useState } from "react"
import { useMe } from "@/graphql/hooks/users"
import {
  useMarkSkillDownloaded,
  useSkillChangelog,
  useSnoozeSkillUpdate,
} from "@/graphql/hooks/skill"
import { decideSkillUpdate } from "@/lib/skill-update"

const SkillUpdatePrompt = lazy(() => import("./skill-update-prompt"))

/**
 * Prompts the operator to re-download the agent skill when the server is
 * handing out a newer release than the one they installed.
 *
 * Mounted globally so it can appear on any authed surface on first load. It
 * shows at most once per release: dismissing it snoozes that release, and the
 * prompt only returns when something newer ships. Someone who never downloaded
 * the skill is never prompted — decideSkillUpdate enforces all of this, and is
 * unit-tested on its own; this component only renders the decision. The
 * dialog itself loads the first time it shows (skill-update-prompt.tsx).
 */
export function SkillUpdateDialog() {
  const [shown, setShown] = useState(false)
  const { data: me } = useMe()
  const { data: changelog } = useSkillChangelog()
  const snooze = useSnoozeSkillUpdate()
  const markDownloaded = useMarkSkillDownloaded()

  if (!me?.me || !changelog) return null

  const decision = decideSkillUpdate({
    currentVersion: changelog.skillChangelog.currentVersion,
    releases: changelog.skillChangelog.releases,
    downloadedVersion: me.me.skillDownloadedVersion,
    snoozedVersion: me.me.skillUpdateSnoozedVersion,
  })

  const current = changelog.skillChangelog.currentVersion

  function dismiss() {
    // Snooze the current release so this exact prompt does not return until a
    // newer one ships. Optimistic, so the dialog closes immediately.
    snooze.mutate(current)
  }

  function handleDownload() {
    // The browser follows the link; the server records the download. Assume
    // success and clear the prompt now, reconciling with the server shortly
    // after (see useMarkSkillDownloaded).
    markDownloaded(current)
  }

  // Shown once, kept mounted: the dialog closes with its own animation
  // when the decision turns, as it always did.
  if (decision.show && !shown) setShown(true)
  if (!shown) return null
  return (
    <Suspense fallback={null}>
      <SkillUpdatePrompt decision={decision} dismiss={dismiss} handleDownload={handleDownload} />
    </Suspense>
  )
}

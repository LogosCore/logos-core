import { lazy, Suspense } from "react"
import { useNavigate } from "react-router"
import { useSkillRegistry, useSnoozeSkill } from "@/graphql/hooks/skills"
import { useMe } from "@/graphql/hooks/users"
import { useSkillChangelog } from "@/graphql/hooks/skill"
import { findSkillsToPromptFor } from "@/lib/community-skill-update"
import { decideSkillUpdate } from "@/lib/skill-update"

const CommunitySkillUpdatePrompt = lazy(() => import("./community-skill-update-prompt"))

/**
 * Tells an operator when a skill they downloaded has been republished.
 *
 * One prompt for every stale skill rather than one prompt each: the number of
 * published skills is not bounded, and a queue of modals is how a useful
 * notification becomes something people close without reading. It also does
 * not offer downloads inline — with several skills that is several decisions,
 * and the page is where they are laid out.
 *
 * Which skills qualify is decided by findSkillsToPromptFor, which is unit
 * tested; this component only renders the result. The dialog itself loads
 * the first time it shows (community-skill-update-prompt.tsx).
 */
export function CommunitySkillUpdateDialog() {
  const navigate = useNavigate()
  const { data } = useSkillRegistry()
  const snooze = useSnoozeSkill()

  const stale = findSkillsToPromptFor(data?.skillRegistry.skills ?? [])
  // Two prompts stacked on a fresh page load is worse than one prompt twice.
  // The built-in skill goes first: it is the one an agent cannot work without.
  const builtinPrompting = useBuiltinSkillPrompting()

  if (stale.length === 0 || builtinPrompting) return null

  // Dismissing covers every skill in the prompt: they were all shown, so
  // asking again about any of them would be asking twice.
  function dismiss() {
    for (const skill of stale) {
      snooze.mutate({ name: skill.name, version: skill.currentVersion })
    }
  }

  function openSkills() {
    dismiss()
    navigate("/skills")
  }

  return (
    <Suspense fallback={null}>
      <CommunitySkillUpdatePrompt stale={stale} dismiss={dismiss} openSkills={openSkills} />
    </Suspense>
  )
}

// Whether the built-in skill's own prompt is currently showing. Read through
// the same decision that dialog uses rather than a shared flag, so the two
// cannot disagree about what is on screen.
function useBuiltinSkillPrompting(): boolean {
  const { data: me } = useMe()
  const { data: changelog } = useSkillChangelog()
  if (!me?.me || !changelog) return false
  return decideSkillUpdate({
    currentVersion: changelog.skillChangelog.currentVersion,
    releases: changelog.skillChangelog.releases,
    downloadedVersion: me.me.skillDownloadedVersion,
    snoozedVersion: me.me.skillUpdateSnoozedVersion,
  }).show
}

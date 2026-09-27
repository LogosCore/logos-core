import { DownloadIcon } from "lucide-react"
import { Link } from "react-router"
import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { useMe } from "@/graphql/hooks/users"
import { useMarkSkillDownloaded, useSkillChangelog } from "@/graphql/hooks/skill"
import { SKILL_DOWNLOAD_URL } from "@/constants/skill"

/**
 * One-click access to the skill that teaches an agent this app, with a badge
 * when the server is handing out a newer release than the operator installed.
 *
 * Deliberately a button and not a section. The Skills page already carries the
 * registry, the version history and the installed-versus-current comparison for
 * every skill on the server; repeating any of that here would be a second copy
 * to keep true. What belongs on the agents page is the one action an operator
 * setting up an agent needs, plus a way through to the rest.
 *
 * A plain link rather than a fetch: the endpoint sits behind the same cookie
 * auth as the rest of the API, and letting the browser handle the download
 * avoids holding a zip in memory to hand straight back to it.
 *
 * An anchor wearing the button styles rather than a Button rendering an anchor.
 * Base UI's button expects a native <button> and says so in the console for
 * every other arrangement; a download is a link, so it stays one.
 */
export function SkillDownloadButton({ size = "default" }: { size?: "default" | "sm" }) {
  const { data: me } = useMe()
  const { data: changelog } = useSkillChangelog()
  const markDownloaded = useMarkSkillDownloaded()

  const current = changelog?.skillChangelog.currentVersion
  const downloaded = me?.me?.skillDownloadedVersion ?? null
  // Behind only counts once something has been installed — a fresh operator is
  // not "out of date", they simply have not started.
  const outdated = current != null && downloaded != null && downloaded < current

  function handleDownload() {
    // The browser follows the link; the server records the download. Assume it
    // worked, clear any "outdated" state now, and reconcile shortly after.
    if (current != null) markDownloaded(current)
  }

  return (
    <a
      href={SKILL_DOWNLOAD_URL}
      download
      onClick={handleDownload}
      className={buttonVariants({ variant: "outline", size })}
    >
      <DownloadIcon className="size-4" />
      {outdated ? "Update skill" : "Download skill"}
      {outdated && (
        <Badge variant="secondary" className="ml-1">
          New
        </Badge>
      )}
    </a>
  )
}

/** Way through to everything else installable, so this page needs no registry. */
export function AllSkillsLink() {
  return (
    <Link to="/skills" className={buttonVariants({ variant: "link", size: "sm" })}>
      See all skills
    </Link>
  )
}

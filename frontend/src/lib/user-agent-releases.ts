// The browsers' release dates behind the User-Agent generator and the check
// under a User-Agent field (user-agent.ts), as their vendors publish them.
// Refresh it all at once and move RELEASES_AS_OF to the day it was read:
// user-agent.test.ts fails once Chrome's, Edge's or Firefox's table has under
// three months left, or Safari's is over six months old.
//
// Sources:
//   Chrome   each milestone's stable_date, from Chromium Dash:
//            https://chromiumdash.appspot.com/fetch_milestone_schedule?offset=-1&n=40
//   Edge     each version's first stable release, or its scheduled one:
//            https://learn.microsoft.com/en-us/deployedge/microsoft-edge-release-schedule
//   Firefox  majors out:
//            https://product-details.mozilla.org/1.0/firefox_history_major_releases.json
//            majors scheduled ("release"):
//            https://whattrainisitnow.com/api/release/schedule/?version=N
//            extended support releases (FIREFOX_ESR, FIREFOX_ESR115):
//            https://product-details.mozilla.org/1.0/firefox_versions.json
//   Safari   iOS's x.y releases, from Apple's security releases:
//            https://support.apple.com/en-us/100100
//            Safari x.y ships with iOS and macOS x.y (Apple lists Safari apart
//            only for older macOS); x.y.z fixes and side releases for older
//            devices (iOS 18.7, 26.7) are left out.
//
// Chrome, Edge and Firefox went from four weeks a version to two in
// September 2026 (Chrome from 153, Edge from 152, Firefox from 155).

/** [version, the day it shipped or is scheduled to, yyyy-mm-dd]. */
export type ReleaseRow = readonly [version: string, date: string]

/** The day these tables were read: a date after it was a schedule then. */
export const RELEASES_AS_OF = "2026-11-09"

export const CHROME_RELEASES: readonly ReleaseRow[] = [
  ["140", "2025-09-02"],
  ["141", "2025-09-30"],
  ["142", "2025-10-28"],
  ["143", "2025-12-02"],
  ["144", "2026-01-13"],
  ["145", "2026-02-10"],
  ["146", "2026-03-10"],
  ["147", "2026-04-07"],
  ["148", "2026-05-05"],
  ["149", "2026-06-02"],
  ["150", "2026-06-30"],
  ["151", "2026-07-28"],
  ["152", "2026-08-25"],
  ["153", "2026-09-08"],
  ["154", "2026-09-22"],
  ["155", "2026-10-06"],
  ["156", "2026-10-20"],
  ["157", "2026-11-03"],
  ["158", "2026-11-17"],
  ["159", "2026-12-08"],
  ["160", "2027-01-05"],
  ["161", "2027-01-19"],
  ["162", "2027-02-02"],
  ["163", "2027-02-16"],
  ["164", "2027-03-02"],
  ["165", "2027-03-16"],
  ["166", "2027-03-30"],
  ["167", "2027-04-13"],
  ["168", "2027-04-27"],
  ["169", "2027-05-11"],
  ["170", "2027-05-25"],
  ["171", "2027-06-08"],
  ["172", "2027-06-22"],
  ["173", "2027-07-06"],
  ["174", "2027-07-20"],
  ["175", "2027-08-03"],
  ["176", "2027-08-17"],
  ["177", "2027-08-31"],
  ["178", "2027-09-14"],
  ["179", "2027-09-28"],
  ["180", "2027-10-12"],
  ["181", "2027-10-26"],
]

export const EDGE_RELEASES: readonly ReleaseRow[] = [
  ["140", "2025-09-05"],
  ["141", "2025-10-02"],
  ["142", "2025-10-31"],
  ["143", "2025-12-04"],
  ["144", "2026-01-16"],
  ["145", "2026-02-14"],
  ["146", "2026-03-13"],
  ["147", "2026-04-10"],
  ["148", "2026-05-07"],
  ["149", "2026-06-04"],
  ["150", "2026-07-02"],
  ["151", "2026-07-31"],
  ["152", "2026-08-27"],
  ["153", "2026-09-10"],
  ["154", "2026-09-24"],
  ["155", "2026-10-08"],
  ["156", "2026-10-22"],
  ["157", "2026-11-05"],
  ["158", "2026-11-19"],
  ["159", "2026-12-10"],
  ["160", "2027-01-07"],
  ["161", "2027-01-21"],
  ["162", "2027-02-04"],
  ["163", "2027-02-18"],
  ["164", "2027-03-04"],
  ["165", "2027-03-18"],
  ["166", "2027-04-01"],
  ["167", "2027-04-15"],
]

export const FIREFOX_RELEASES: readonly ReleaseRow[] = [
  ["143", "2025-09-16"],
  ["144", "2025-10-14"],
  ["145", "2025-11-11"],
  ["146", "2025-12-09"],
  ["147", "2026-01-13"],
  ["148", "2026-02-24"],
  ["149", "2026-03-24"],
  ["150", "2026-04-21"],
  ["151", "2026-05-19"],
  ["152", "2026-06-16"],
  ["153", "2026-07-21"],
  ["154", "2026-08-18"],
  ["155", "2026-09-01"],
  ["156", "2026-09-15"],
  ["157", "2026-09-29"],
  ["158", "2026-10-13"],
  ["159", "2026-10-27"],
  ["160", "2026-11-10"],
  ["161", "2026-11-24"],
  ["162", "2026-12-08"],
  ["163", "2027-01-12"],
  ["164", "2027-01-26"],
  ["165", "2027-02-09"],
  ["166", "2027-02-23"],
  ["167", "2027-03-09"],
  ["168", "2027-03-23"],
  ["169", "2027-04-06"],
  ["170", "2027-04-20"],
  ["171", "2027-05-04"],
  ["172", "2027-05-18"],
  ["173", "2027-06-01"],
  ["174", "2027-06-15"],
  ["175", "2027-06-29"],
  ["176", "2027-07-13"],
  ["177", "2027-07-27"],
  ["178", "2027-08-10"],
  ["179", "2027-08-24"],
  ["180", "2027-09-07"],
  ["181", "2027-09-21"],
  ["182", "2027-10-05"],
  ["183", "2027-10-19"],
  ["184", "2027-11-02"],
  ["185", "2027-11-16"],
]

/**
 * Firefox's extended support releases still supported: the majors companies
 * run for a year (153), and Windows 7 and 8's last (115). A UA of one is not
 * behind.
 */
export const FIREFOX_ESR: readonly number[] = [115, 153]

export const SAFARI_RELEASES: readonly ReleaseRow[] = [
  ["18.0", "2024-09-16"],
  ["18.1", "2024-10-28"],
  ["18.2", "2024-12-11"],
  ["18.3", "2025-01-27"],
  ["18.4", "2025-03-31"],
  ["18.5", "2025-05-12"],
  ["18.6", "2025-07-29"],
  ["26.0", "2025-09-15"],
  ["26.1", "2025-11-03"],
  ["26.2", "2025-12-12"],
  ["26.3", "2026-02-11"],
  ["26.4", "2026-03-24"],
  ["26.5", "2026-05-11"],
  ["26.6", "2026-07-27"],
  ["27.0", "2026-09-14"],
  ["27.1", "2026-10-23"],
  ["27.2", "2026-10-26"],
]

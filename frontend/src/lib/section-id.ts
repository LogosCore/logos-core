/** The id of a form section, from its title: "Dummy builds" → "section-dummy-builds". */
export function sectionId(title: string): string {
  return `section-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`
}

// Saving a file the browser built, rather than one it fetched.
//
// The Markdown dialog has its own copy of this because it hands over a string
// it already has on screen; a drawing has no dialog — the menu item is the
// whole interaction — so the download lives here, next to the menu.

/** Save text as a file named after the page. */
export function downloadTextFile(
  filename: string,
  contents: string,
  mimeType: string,
): void {
  const blob = new Blob([contents], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  // Revoking synchronously can cancel the download in some browsers, so give
  // the click a turn of the event loop first.
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

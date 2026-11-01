// Commands that check a downloaded file against its SHA-256, ready to
// paste in the folder the file was saved to: each says OK, or fails.

export interface VerifyCommand {
  /** Where it runs. */
  label: string
  command: string
}

/** The name a download is saved under: core's (minion_task_controller.go,
 *  downloadName), the path's last part as it is (Cyrillic, spaces and
 *  all), its control characters dropped; fallback for none. A browser may
 *  still swap what its file system forbids (on Windows : * ? " < > |). */
export function savedName(path: string, fallback: string): string {
  const base = path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1)
  // eslint-disable-next-line no-control-regex -- the control characters are the point
  const name = base.replace(/[\u0000-\u001f\u007f]/g, "")
  return name === "" || name === "." || name === ".." ? fallback : name
}

/** A string for a POSIX shell, single-quoted. */
export function shellQuote(s: string): string {
  return `'${s.replaceAll("'", `'\\''`)}'`
}

/** A string for PowerShell, single-quoted. */
function psQuote(s: string): string {
  return `'${s.replaceAll("'", "''")}'`
}

/** The commands that check the file name (as saved) against sha256 (hex). */
export function verifyCommands(name: string, sha256: string): VerifyCommand[] {
  const hex = sha256.toLowerCase()
  // sha256sum -c and shasum -c read "<hash>  <name>" lines: two spaces.
  const line = shellQuote(`${hex}  ${name}`)
  return [
    { label: "Linux", command: `echo ${line} | sha256sum -c -` },
    { label: "macOS", command: `echo ${line} | shasum -a 256 -c -` },
    {
      label: "Windows",
      command: `if ((Get-FileHash -Algorithm SHA256 -LiteralPath ${psQuote(name)}).Hash -eq ${psQuote(hex.toUpperCase())}) { 'OK' } else { 'MISMATCH' }`,
    },
  ]
}

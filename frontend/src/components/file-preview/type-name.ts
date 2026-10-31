// A file's kind in words ("Word document", "ELF executable"), from its
// content type as core's sniffer (mimetype) or an upload names it, else its
// extension: what a person reads where the MIME type is too long to.

const NAMES: Record<string, string> = {
  "application/octet-stream": "Binary data",
  "application/pdf": "PDF document",
  "application/msword": "Word document",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "Word document",
  "application/vnd.ms-excel": "Excel workbook",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "Excel workbook",
  "application/vnd.ms-powerpoint": "PowerPoint presentation",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation": "PowerPoint presentation",
  "application/vnd.oasis.opendocument.text": "OpenDocument text",
  "application/vnd.oasis.opendocument.spreadsheet": "OpenDocument spreadsheet",
  "application/rtf": "RTF document",
  "text/rtf": "RTF document",
  "application/vnd.ms-outlook": "Outlook message",
  "application/x-ole-storage": "OLE compound file",
  "application/zip": "ZIP archive",
  "application/x-tar": "tar archive",
  "application/gzip": "gzip archive",
  "application/x-gzip": "gzip archive",
  "application/x-bzip2": "bzip2 archive",
  "application/x-xz": "xz archive",
  "application/zstd": "Zstandard archive",
  "application/x-7z-compressed": "7-Zip archive",
  "application/x-rar-compressed": "RAR archive",
  "application/vnd.rar": "RAR archive",
  "application/java-archive": "Java archive",
  "application/vnd.android.package-archive": "Android package",
  "application/x-debian-package": "Debian package",
  "application/x-rpm": "RPM package",
  "application/x-iso9660-image": "ISO disk image",
  "application/x-elf": "ELF file",
  "application/x-executable": "ELF executable",
  "application/x-sharedlib": "ELF shared library",
  "application/x-object": "ELF object",
  "application/x-coredump": "ELF core dump",
  "application/vnd.microsoft.portable-executable": "Windows executable (PE)",
  "application/x-msdownload": "Windows executable (PE)",
  "application/x-dosexec": "Windows executable (PE)",
  "application/x-mach-binary": "Mach-O binary",
  "application/x-ms-shortcut": "Windows shortcut",
  "application/x-sqlite3": "SQLite database",
  "application/vnd.tcpdump.pcap": "Packet capture",
  "application/x-pcapng": "Packet capture",
  "application/json": "JSON",
  "application/ld+json": "JSON",
  "application/xml": "XML",
  "text/xml": "XML",
  "text/html": "HTML document",
  "application/xhtml+xml": "HTML document",
  "text/plain": "Text",
  "text/markdown": "Markdown",
  "text/csv": "CSV",
  "text/tab-separated-values": "TSV",
  "text/javascript": "JavaScript",
  "application/javascript": "JavaScript",
  "text/x-shellscript": "Shell script",
  "text/x-python": "Python script",
  "text/x-perl": "Perl script",
  "text/x-php": "PHP script",
  "text/x-lua": "Lua script",
  "text/x-tcl": "Tcl script",
  "application/x-pem-file": "PEM file",
  "application/pgp-keys": "PGP keys",
  "image/svg+xml": "SVG image",
  "image/x-icon": "Icon",
  "image/vnd.microsoft.icon": "Icon",
  "audio/mpeg": "MP3 audio",
  "audio/x-wav": "WAV audio",
  "audio/wav": "WAV audio",
  "video/quicktime": "QuickTime video",
}

const EXTENSIONS: Record<string, string> = {
  exe: "Windows executable (PE)",
  dll: "Windows library (PE)",
  sys: "Windows driver (PE)",
  so: "Shared library",
  ps1: "PowerShell script",
  bat: "Batch script",
  cmd: "Batch script",
  vbs: "VBScript",
  sh: "Shell script",
  py: "Python script",
  yaml: "YAML",
  yml: "YAML",
  toml: "TOML",
  ini: "INI file",
  conf: "Configuration file",
  cfg: "Configuration file",
  log: "Log",
  key: "Key file",
  pem: "PEM file",
  crt: "Certificate",
  cer: "Certificate",
  kdbx: "KeePass database",
  ovpn: "OpenVPN profile",
}

/** A file's kind in a few words; null when nothing says. */
export function fileTypeName(contentType: string, filename: string): string | null {
  const ct = contentType.split(";")[0].trim().toLowerCase()
  const ext = filename.includes(".") ? (filename.split(".").pop() ?? "").toLowerCase() : ""
  // A generic sniff yields to what the name says.
  if ((ct === "" || ct === "application/octet-stream" || ct === "text/plain") && EXTENSIONS[ext]) return EXTENSIONS[ext]
  if (NAMES[ct]) return NAMES[ct]
  const m = /^(image|audio|video)\/(?:x-)?([a-z0-9.+-]+)$/.exec(ct)
  if (m) return `${m[2].toUpperCase()} ${m[1]}`
  if (EXTENSIONS[ext]) return EXTENSIONS[ext]
  return ext ? `${ext.toUpperCase()} file` : null
}

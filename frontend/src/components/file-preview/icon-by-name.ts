import {
  DatabaseIcon,
  FileArchiveIcon,
  FileAudioIcon,
  FileCodeIcon,
  FileCogIcon,
  FileIcon,
  FileJsonIcon,
  FileKeyIcon,
  FileSpreadsheetIcon,
  FileTerminalIcon,
  FileTextIcon,
  FileVideoIcon,
  ImageIcon,
  type LucideIcon,
} from "lucide-react"

// Kinds by extension, for the files hosts hold: an operator's eye goes to
// keys, databases and programs as much as to pictures and documents.
const BY_EXT: [LucideIcon, string[]][] = [
  [ImageIcon, ["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "ico", "heic", "heif", "tif", "tiff", "avif", "psd", "raw", "cr2", "nef"]],
  [FileVideoIcon, ["mp4", "mkv", "avi", "mov", "webm", "m4v", "wmv", "flv", "mpg", "mpeg", "3gp", "m2ts"]],
  [FileAudioIcon, ["mp3", "wav", "flac", "ogg", "oga", "m4a", "aac", "opus", "wma", "aiff", "mid", "midi"]],
  [FileArchiveIcon, ["zip", "tar", "gz", "tgz", "bz2", "xz", "zst", "7z", "rar", "cab", "iso", "img", "dmg", "jar", "war", "deb", "rpm", "apk", "lz", "lzma"]],
  [FileSpreadsheetIcon, ["xls", "xlsx", "xlsm", "ods", "csv", "tsv", "numbers"]],
  [FileTextIcon, ["doc", "docx", "odt", "rtf", "pdf", "txt", "md", "log", "pages", "ppt", "pptx", "odp", "epub", "tex"]],
  [FileJsonIcon, ["json", "jsonl", "ndjson"]],
  [
    FileCodeIcon,
    [
      "html", "htm", "xml", "js", "mjs", "cjs", "jsx", "ts", "tsx", "py", "go", "c", "h", "cc", "cpp", "hpp", "cs", "java", "kt",
      "rb", "php", "pl", "lua", "rs", "swift", "scala", "css", "scss", "sql", "yaml", "yml", "toml", "ini", "cfg", "conf",
      "properties", "env", "vue", "svelte",
    ],
  ],
  [FileTerminalIcon, ["sh", "bash", "zsh", "fish", "ps1", "psm1", "bat", "cmd", "vbs"]],
  [FileCogIcon, ["exe", "dll", "so", "dylib", "sys", "msi", "bin", "elf", "o", "a", "lib", "ko", "com", "scr", "drv"]],
  [FileKeyIcon, ["pem", "key", "crt", "cer", "der", "p12", "pfx", "ppk", "pub", "asc", "gpg", "kdbx", "kdb", "keystore", "jks", "ovpn"]],
  [DatabaseIcon, ["db", "sqlite", "sqlite3", "mdb", "accdb", "dump", "bak", "dbf", "ldb", "frm", "ibd"]],
]
const EXT = new Map(BY_EXT.flatMap(([icon, exts]) => exts.map((e) => [e, icon] as const)))

// Names that say what they are without an extension.
const KEY_NAMES = /^(id_(rsa|dsa|ecdsa|ed25519)(-cert)?|authorized_keys|known_hosts|shadow|gshadow|passwd|\.htpasswd|\.netrc|\.pgpass|ntds\.dit|sam|system|security)$/i

/** The icon of a file's kind by its name alone. */
export function iconByName(filename: string): LucideIcon {
  const name = filename.split(/[\\/]/).pop() ?? ""
  if (KEY_NAMES.test(name)) return FileKeyIcon
  const at = name.lastIndexOf(".")
  if (at <= 0) return FileIcon
  return EXT.get(name.slice(at + 1).toLowerCase()) ?? FileIcon
}

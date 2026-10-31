import {
  FileArchiveIcon,
  FileAudioIcon,
  FileCodeIcon,
  FileSpreadsheetIcon,
  FileTextIcon,
  FileVideoIcon,
  ImageIcon,
  type LucideIcon,
} from "lucide-react"
import { createElement, type ReactElement } from "react"
import { iconByName } from "./icon-by-name"

/** The icon of a file's kind, from its content type, else its name. */
export function FileTypeIcon({
  contentType,
  filename,
  size,
  className,
}: {
  contentType: string
  filename: string
  size?: number
  className?: string
}): ReactElement {
  return createElement(fileTypeIcon(contentType, filename), { size, className })
}

function fileTypeIcon(contentType: string, filename: string): LucideIcon {
  const type = (contentType ?? "").toLowerCase()

  if (type.startsWith("audio/")) return FileAudioIcon
  if (type.startsWith("video/")) return FileVideoIcon
  // Reached by images that have no thumbnail: a format we have no decoder
  // for, one still decoding or that failed to, or one whose bytes have gone
  // missing.
  if (type.startsWith("image/")) return ImageIcon
  if (
    type === "text/html" ||
    type === "application/xhtml+xml" ||
    type === "application/xml" ||
    type === "text/xml"
  ) {
    return FileCodeIcon
  }
  if (
    type.startsWith("text/") ||
    type === "application/json" ||
    type === "application/pdf" ||
    type === "application/msword" ||
    type.includes("wordprocessingml")
  ) {
    return FileTextIcon
  }
  if (type.includes("spreadsheet") || type === "text/csv" || type === "application/vnd.ms-excel") {
    return FileSpreadsheetIcon
  }
  if (
    type === "application/zip" ||
    type === "application/x-tar" ||
    type === "application/x-7z-compressed" ||
    type === "application/x-rar-compressed" ||
    type === "application/gzip"
  ) {
    return FileArchiveIcon
  }

  // Fall back to the name when the type is not known: a file core does
  // not keep (the file browser's), or one the server couldn't identify.
  return iconByName(filename)
}

/** The icon of a file's kind by its name alone: for files whose type is not known. */
export function FileNameIcon({ name, className }: { name: string; className?: string }): ReactElement {
  return createElement(iconByName(name), { className })
}

import {
  FileArchiveIcon,
  FileAudioIcon,
  FileCodeIcon,
  FileIcon,
  FileSpreadsheetIcon,
  FileTextIcon,
  FileVideoIcon,
  ImageIcon,
  type LucideIcon,
} from "lucide-react"
import { createElement, type ReactElement } from "react"

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

  // Fall back to the extension when the server couldn't identify the type.
  const ext = filename.toLowerCase().split(".").pop() ?? ""
  if (["zip", "tar", "gz", "7z", "rar"].includes(ext)) return FileArchiveIcon
  if (["xls", "xlsx", "csv", "tsv"].includes(ext)) return FileSpreadsheetIcon
  if (["doc", "docx", "txt", "md", "rtf", "pdf"].includes(ext)) return FileTextIcon
  return FileIcon
}

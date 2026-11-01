import { UploadIcon } from "lucide-react"
import type { MenuCommand } from "./action-menu"
import { ActionButton } from "./action-button"

/** A folder's Upload here, in a view's tool row (action-button.tsx). */
export function UploadHereButton({ folder, commands, onRun }: { folder: string; commands: readonly MenuCommand[]; onRun: (commandId: string) => void }) {
  return <ActionButton icon={<UploadIcon />} label="Upload here" title={`Upload a file to ${folder}`} commands={commands} onRun={onRun} />
}

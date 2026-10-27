import { useRef, useState } from "react"
import { useNavigate } from "react-router"
import { FileUpIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { ENDPOINT_EXPORT_FORMAT, parseEndpointImport, type EndpointImportState, type Kind } from "./endpoint-values"

/**
 * Import an endpoint from its JSON export (Copy / Download as JSON): pasted or
 * from a file. A good one opens the New endpoint form filled in from it, to
 * check before it is saved.
 */
export function ImportEndpointDialog({
  open,
  onClose,
  kinds,
  taken,
}: {
  open: boolean
  onClose: () => void
  kinds: ReadonlyMap<string, Kind>
  /** The operation's endpoint names, for a free name. */
  taken: readonly string[]
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {/* Keyed so each opening starts empty. */}
        {open && <ImportBody key="import" kinds={kinds} taken={taken} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function ImportBody({
  kinds,
  taken,
  onClose,
}: {
  kinds: ReadonlyMap<string, Kind>
  taken: readonly string[]
  onClose: () => void
}) {
  const navigate = useNavigate()
  const file = useRef<HTMLInputElement>(null)
  const [text, setText] = useState("")
  const [error, setError] = useState<string | null>(null)

  function proceed() {
    const r = parseEndpointImport(text, kinds, taken)
    if (!r.ok) {
      setError(r.error)
      return
    }
    const { kind, name, values, notes } = r.value
    const state: EndpointImportState = { endpointImport: { name, values, notes } }
    onClose()
    navigate(`/endpoints/new?kind=${encodeURIComponent(kind.name)}`, { state })
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Import an endpoint</DialogTitle>
        <DialogDescription>
          Paste an endpoint copied as JSON, or choose a file downloaded as JSON. It opens as a new endpoint for you to
          check before saving.
        </DialogDescription>
      </DialogHeader>
      <Textarea
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          setError(null)
        }}
        rows={10}
        spellCheck={false}
        placeholder={`{ "format": "${ENDPOINT_EXPORT_FORMAT}", … }`}
        aria-label="Endpoint JSON"
        className="font-mono text-xs"
      />
      <div>
        <input
          ref={file}
          type="file"
          accept=".json,application/json"
          className="hidden"
          onChange={async (e) => {
            const f = e.target.files?.[0]
            e.target.value = ""
            if (!f) return
            setText(await f.text())
            setError(null)
          }}
        />
        <Button type="button" variant="outline" size="sm" onClick={() => file.current?.click()}>
          <FileUpIcon />
          Choose file
        </Button>
      </div>
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button onClick={proceed} disabled={text.trim() === ""}>
          Continue
        </Button>
      </DialogFooter>
    </>
  )
}

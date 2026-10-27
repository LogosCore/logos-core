import { createContext, useContext, useEffect, useState, type ReactNode, type Ref } from "react"
import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

// The frame of the build and endpoint modals (components/layout/
// record-modals.tsx): near full screen, a header row with the record's title,
// its actions and a close button, and a body that scrolls. A form in it
// reports unsaved changes with useCloseGuard; closing it then asks first,
// whichever way it is closed (Esc, a click outside, the close button, Cancel).

const RequestCloseContext = createContext<() => void>(() => {})
const GuardContext = createContext<(dirty: boolean) => void>(() => {})

/** Closes the modal, asking first when its form has unsaved changes. */
export function useRequestClose(): () => void {
  return useContext(RequestCloseContext)
}

/** Marks the modal's form as having unsaved changes while dirty is true. */
export function useCloseGuard(dirty: boolean) {
  const setDirty = useContext(GuardContext)
  useEffect(() => {
    setDirty(dirty)
    return () => setDirty(false)
  }, [dirty, setDirty])
}

export function RecordModal({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  const [dirty, setDirty] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const requestClose = () => (dirty ? setConfirming(true) : onClose())

  return (
    <Dialog open onOpenChange={(open) => !open && requestClose()}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[calc(100svh-2rem)] max-w-[calc(100%-2rem)] flex-col gap-0 overflow-hidden p-0 sm:h-[calc(100svh-4rem)] sm:max-w-[calc(100%-4rem)] max-sm:h-svh max-sm:max-w-none max-sm:rounded-none"
      >
        <RequestCloseContext.Provider value={requestClose}>
          <GuardContext.Provider value={setDirty}>{children}</GuardContext.Provider>
        </RequestCloseContext.Provider>
        <DiscardChangesDialog
          open={confirming}
          onKeep={() => setConfirming(false)}
          onDiscard={() => {
            setConfirming(false)
            onClose()
          }}
        />
      </DialogContent>
    </Dialog>
  )
}

/** The header row: what children lay out (title, actions), then the close button. */
export function RecordModalHeader({ children }: { children: ReactNode }) {
  const requestClose = useRequestClose()
  return (
    <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b px-4 py-3">
      {children}
      <Button type="button" variant="ghost" size="icon-sm" aria-label="Close" title="Close" onClick={requestClose}>
        <XIcon />
      </Button>
    </header>
  )
}

/** The record's title, which names the dialog. */
export function RecordModalTitle({ children }: { children: ReactNode }) {
  return <DialogTitle className="text-lg leading-normal font-semibold">{children}</DialogTitle>
}

/** The scrolling body; ref is its scroll container (for useActiveSection). */
export function RecordModalBody({ children, ref }: { children: ReactNode; ref?: Ref<HTMLDivElement> }) {
  return (
    <div ref={ref} className="min-h-0 flex-1 overflow-y-auto">
      {children}
    </div>
  )
}

/** What a record that cannot be shown says instead, under a header with the close button. */
export function RecordModalMessage({ icon, title, text }: { icon: ReactNode; title: string; text: string }) {
  return (
    <>
      <RecordModalHeader>
        <RecordModalTitle>{title}</RecordModalTitle>
        <span className="flex-1" />
      </RecordModalHeader>
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
        {icon}
        <p className="max-w-md text-sm">{text}</p>
      </div>
    </>
  )
}

function DiscardChangesDialog({ open, onKeep, onDiscard }: { open: boolean; onKeep: () => void; onDiscard: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onKeep()}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Discard your changes?</DialogTitle>
          <DialogDescription>What you entered in this form is not saved.</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onKeep}>
            Keep editing
          </Button>
          <Button type="button" variant="destructive" onClick={onDiscard}>
            Discard
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

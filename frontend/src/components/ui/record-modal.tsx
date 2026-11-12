import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode, type Ref } from "react"
import { useBlocker } from "react-router"
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
import { leavesRecordModal } from "@/lib/record-modals"
import { cn } from "@/lib/utils"

// The frame of the build and endpoint modals (components/layout/
// record-modals.tsx): near full screen (a modal may narrow it with className:
// the endpoint form's), a header row with the record's title,
// its actions and a close button, and a body that scrolls. It appears and
// goes at once: duration-0 zeroes the fade and zoom the other dialogs have.
//
// A form in it reports unsaved changes with useCloseGuard. A navigation that
// would then close the modal, or put another in its place, asks first. That
// covers every way out: Esc, a click outside, the close button and Cancel
// all close it by navigating, and so do Back, Forward and links. Reloading or
// leaving the site gets the browser's own prompt instead.

const RequestCloseContext = createContext<() => void>(() => {})
const GuardContext = createContext<(dirty: boolean) => void>(() => {})
const SavedContext = createContext<() => void>(() => {})

/** Closes the modal; with unsaved changes in its form, asks first. */
export function useRequestClose(): () => void {
  return useContext(RequestCloseContext)
}

/**
 * Marks the modal's form as having unsaved changes while dirty is true.
 * Returns saved(): call it after a save, just before closing the modal or
 * moving it on to what was saved, so that one navigation does not ask.
 */
export function useCloseGuard(dirty: boolean): () => void {
  const setDirty = useContext(GuardContext)
  useEffect(() => {
    setDirty(dirty)
    return () => setDirty(false)
  }, [dirty, setDirty])
  return useContext(SavedContext)
}

/** RecordModal's className for a form: one column of setting rows, labels beside the fields. */
export const RECORD_FORM_WIDTH = "sm:max-w-[min(calc(100%-4rem),64rem)]"

export function RecordModal({
  onClose,
  className,
  children,
}: {
  onClose: () => void
  /** Added to the dialog's own, after them: a max width, say. */
  className?: string
  children: ReactNode
}) {
  const [dirty, setDirty] = useState(false)
  // One navigation let through, set by saved(). Whichever navigation comes
  // next uses it up, so it cannot wait around for a later one.
  const pass = useRef(false)
  const saved = useCallback(() => {
    pass.current = true
  }, [])
  const blocker = useBlocker(({ currentLocation, nextLocation }) => {
    const passing = pass.current
    pass.current = false
    return dirty && !passing && leavesRecordModal(currentLocation, nextLocation)
  })

  useEffect(() => {
    if (!dirty) return
    const ask = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      // Still required by some browsers to show the prompt at all.
      e.returnValue = ""
    }
    window.addEventListener("beforeunload", ask)
    return () => window.removeEventListener("beforeunload", ask)
  }, [dirty])

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        showCloseButton={false}
        // The page behind it dimmed (not blurred), so the record in front is
        // what reads: darker in the dark theme, where the page already is.
        overlayClassName="bg-black/50 duration-0 dark:bg-black/70"
        className={cn(
          "flex h-[calc(100svh-2rem)] max-w-[calc(100%-2rem)] flex-col gap-0 overflow-hidden p-0 sm:h-[calc(100svh-4rem)] sm:max-w-[calc(100%-4rem)] max-sm:h-svh max-sm:max-w-none max-sm:rounded-none duration-0",
          className,
        )}
      >
        <RequestCloseContext.Provider value={onClose}>
          <GuardContext.Provider value={setDirty}>
            <SavedContext.Provider value={saved}>{children}</SavedContext.Provider>
          </GuardContext.Provider>
        </RequestCloseContext.Provider>
        <DiscardChangesDialog
          open={blocker.state === "blocked"}
          onKeep={() => blocker.reset?.()}
          onDiscard={() => blocker.proceed?.()}
        />
      </DialogContent>
    </Dialog>
  )
}

/**
 * The header row: what children lay out (title, actions), then the close
 * button. below goes under that row, on the header's rule: a record's tabs
 * (RECORD_TABS_LIST, RECORD_TABS_TRIGGER).
 */
export function RecordModalHeader({ children, below }: { children: ReactNode; below?: ReactNode }) {
  const requestClose = useRequestClose()
  const row = (
    <>
      {children}
      <Button type="button" variant="ghost" size="icon-sm" aria-label="Close" title="Close" onClick={requestClose}>
        <XIcon />
      </Button>
    </>
  )
  if (!below) {
    return <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b px-4 py-3">{row}</header>
  }
  return (
    <header className="shrink-0 border-b">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 pt-3 pb-1">{row}</div>
      <div className="px-4">{below}</div>
    </header>
  )
}

// Tabs as a bar under a record's title (RecordModalHeader's below), the
// open one underlined on the header's rule: TabsList's and TabsTrigger's
// classNames.
export const RECORD_TABS_LIST = "h-auto gap-5 rounded-none bg-transparent p-0"
export const RECORD_TABS_TRIGGER =
  "-mb-px h-9 rounded-none border-b-2 border-transparent px-0.5 hover:text-foreground data-active:border-foreground data-active:bg-transparent data-active:shadow-none"

/** The record's title, which names the dialog. */
export function RecordModalTitle({ children }: { children: ReactNode }) {
  return <DialogTitle className="text-lg leading-normal font-semibold">{children}</DialogTitle>
}

/** The scrolling body; ref is its scroll container. */
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

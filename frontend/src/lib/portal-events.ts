// A popup (a dialog, a sheet, a popover, a menu) is drawn elsewhere in the
// DOM (a portal), but React carries its events on through the components it
// was opened from, as though it were inside them. A form there gets the
// popup's submits, a key handler its keys: a credential picker's "create"
// form submitted the command form whose field it was picking for, and the
// picker's Ctrl+Enter ran the command.

/**
 * A popup's onSubmit: a form in it submits itself alone, its own handler
 * (onSubmit, when given) first.
 */
export function keepSubmitIn<E extends { stopPropagation(): void }>(onSubmit?: (e: E) => void): (e: E) => void {
  return (e) => {
    onSubmit?.(e)
    e.stopPropagation()
  }
}

/**
 * Whether an event began inside the element whose handler runs, as the DOM
 * has it: not in a popup opened from it, whose keys are the popup's.
 */
export function fromWithin(e: { currentTarget: EventTarget; target: EventTarget }): boolean {
  return (e.currentTarget as Node).contains(e.target as Node)
}

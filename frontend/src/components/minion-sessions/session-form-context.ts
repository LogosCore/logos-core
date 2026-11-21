import { createContext, useContext } from "react"

// The session a command form belongs to (SessionCommandDialog): what a
// field that needs the session's factory and channels (a connection param,
// connection-field.tsx) reads them by. null outside a session's form.

export const SessionFormContext = createContext<string | null>(null)

/** The session the form is for; null outside a session's command form. */
export function useSessionFormId(): string | null {
  return useContext(SessionFormContext)
}

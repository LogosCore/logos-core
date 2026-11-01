import type { ReactNode } from "react"
import type { ConnectionChoice } from "./connection-choices"
import { ConnectionChoicesContext } from "./use-connection-choices"

/** Gives the command forms under it the session's connections, for a connection param. */
export function ConnectionChoicesProvider({ choices, children }: { choices: readonly ConnectionChoice[]; children: ReactNode }) {
  return <ConnectionChoicesContext.Provider value={choices}>{children}</ConnectionChoicesContext.Provider>
}

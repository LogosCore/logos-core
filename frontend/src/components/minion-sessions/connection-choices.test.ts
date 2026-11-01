import { describe, expect, test } from "vitest"
import { lineOf, parseCommandLine, valueCompletion, type CommandDef } from "./command-line"
import { connectionFromText, connectionText, matchConnections, type ConnectionChoice } from "./connection-choices"

const connect: CommandDef = {
  id: "connect",
  label: "Switch connection",
  params: [
    { key: "conn", type: "int", label: "Connection", required: true, format: "connection" },
    { key: "for", type: "duration", label: "For", required: false, default: "0s" },
  ],
}
const commands: CommandDef[] = [connect, { id: "ls", label: "List", params: [{ key: "path", type: "string", label: "Path", required: false }] }]

const connections: ConnectionChoice[] = [
  { place: 1, name: "poll", kind: "reverse-call", address: "http://edge:8090", inUse: true },
  { place: 2, name: "ws", kind: "reverse-persistent", address: "ws://edge:8090" },
  { place: 3, name: "edge-eu", kind: "reverse-call" },
  { place: 4, name: "edge-eu", kind: "reverse-persistent" },
]

describe("a connection param", () => {
  test("takes a name, case aside, or a place; a shared name needs the place", () => {
    expect(connectionFromText("conn", "WS", connections)).toEqual({ ok: true, value: 2 })
    expect(connectionFromText("conn", "3", connections)).toEqual({ ok: true, value: 3 })
    expect(connectionFromText("conn", "edge-eu", connections)).toEqual({
      ok: false,
      error: "conn: 2 connections are called edge-eu: use its number (3, 4)",
    })
    expect(connectionFromText("conn", "dns", connections)).toEqual({ ok: false, error: "conn: no connection dns: poll, ws, edge-eu, edge-eu" })
  })

  test("reads in a line, and is shown back by name when the name is its alone", () => {
    expect(parseCommandLine("connect ws --for=10m", commands, connections)).toMatchObject({ ok: true, args: { conn: 2, for: "10m" } })
    expect(parseCommandLine("connect --conn=poll", commands, connections)).toMatchObject({ ok: true, args: { conn: 1 } })
    expect(lineOf("connect", { conn: 2, for: "10m" }, connect, connections)).toBe("connect --conn=ws --for=10m")
    expect(lineOf("connect", { conn: 4 }, connect, connections)).toBe("connect --conn=4")
    expect(connectionText(9, connections)).toBe("9")
    // Without the session's connections: the place, as before.
    expect(lineOf("connect", { conn: 2 }, connect)).toBe("connect --conn=2")
  })

  test("offers the connections as the value is typed, the one in use last", () => {
    expect(valueCompletion("connect ", commands, connections)).toMatchObject({
      key: "conn",
      start: 8,
      partial: "",
      options: [{ place: 2 }, { place: 3 }, { place: 4 }, { place: 1 }],
    })
    expect(valueCompletion("connect e", commands, connections)?.options.map((c) => c.place)).toEqual([3, 4])
    expect(valueCompletion("connect --conn=w", commands, connections)).toMatchObject({ start: 15, partial: "w", options: [{ name: "ws" }] })
    expect(valueCompletion("connect --for 10m ", commands, connections)?.key).toBe("conn")
    expect(valueCompletion("connect --conn ", commands, connections)?.key).toBe("conn")
    // Not for the command word, another param, or another command.
    expect(valueCompletion("conn", commands, connections)).toBeNull()
    expect(valueCompletion("connect ws ", commands, connections)).toBeNull()
    expect(valueCompletion("ls ", commands, connections)).toBeNull()
    expect(valueCompletion("connect ", commands, [])).toBeNull()
  })

  test("matches by name or place", () => {
    expect(matchConnections("2", connections).map((c) => c.name)).toEqual(["ws"])
    expect(matchConnections("P", connections).map((c) => c.name)).toEqual(["poll"])
  })
})

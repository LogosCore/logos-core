import { describe, expect, test } from "vitest"
import { lineOf, parseCommandLine, valueCompletion, type CommandDef } from "./command-line"
import { choiceTitle, connectionFromText, connectionJSON, connectionText, matchConnections, type ConnectionChoice } from "./connection-choices"

const connect: CommandDef = {
  id: "connect",
  label: "Switch connection",
  params: [
    { key: "conn", type: "int", label: "Connection", required: true, format: "connection" },
    { key: "for", type: "duration", label: "For", required: false, default: "0s" },
  ],
}
const commands: CommandDef[] = [connect, { id: "ls", label: "List", params: [{ key: "path", type: "string", label: "Path", required: false }] }]

// A session's connections have no names: each is its place, with its channel, kind and address.
const connections: ConnectionChoice[] = [
  { place: 1, channel: "http", kind: "reverse-call", address: "http://edge:8090", inUse: true },
  { place: 2, channel: "http", kind: "reverse-persistent", address: "ws://edge:8090" },
  { place: 3, channel: "tcp", kind: "reverse-call" },
  { place: 4, channel: "tcp", kind: "reverse-persistent" },
  { place: 10, channel: "dns", kind: "reverse-call" },
]

describe("a connection param", () => {
  test("takes a place, one of the session's", () => {
    expect(connectionFromText("conn", "3", connections)).toEqual({ ok: true, value: 3 })
    expect(connectionFromText("conn", " 2 ", connections)).toEqual({ ok: true, value: 2 })
    expect(connectionFromText("conn", "ws", connections)).toEqual({ ok: false, error: "conn: a connection's number, 1 to 10" })
    expect(connectionFromText("conn", "7", connections)).toEqual({ ok: false, error: "conn: a connection's number, 1 to 10" })
    // The session's connections not known: any place.
    expect(connectionFromText("conn", "7", [])).toEqual({ ok: true, value: 7 })
  })

  test("reads in a line, and is shown back as its place", () => {
    expect(parseCommandLine("connect 2 --for=10m", commands, connections)).toMatchObject({ ok: true, args: { conn: 2, for: "10m" } })
    expect(parseCommandLine("connect --conn=1", commands, connections)).toMatchObject({ ok: true, args: { conn: 1 } })
    expect(lineOf("connect", { conn: 2, for: "10m" }, connect)).toBe("connect --conn=2 --for=10m")
    expect(connectionText(9)).toBe("9")
  })

  test("offers the connections as the value is typed, the one in use last", () => {
    expect(valueCompletion("connect ", commands, connections)).toMatchObject({
      key: "conn",
      start: 8,
      partial: "",
      options: [{ place: 2 }, { place: 3 }, { place: 4 }, { place: 10 }, { place: 1 }],
    })
    expect(valueCompletion("connect --conn=1", commands, connections)).toMatchObject({ start: 15, partial: "1", options: [{ place: 10 }, { place: 1 }] })
    expect(valueCompletion("connect --for 10m ", commands, connections)?.key).toBe("conn")
    expect(valueCompletion("connect --conn ", commands, connections)?.key).toBe("conn")
    // Not for the command word, another param, or another command.
    expect(valueCompletion("conn", commands, connections)).toBeNull()
    expect(valueCompletion("connect 2 ", commands, connections)).toBeNull()
    expect(valueCompletion("ls ", commands, connections)).toBeNull()
    expect(valueCompletion("connect ", commands, [])).toBeNull()
  })

  test("matches by place, and shows each by place and channel", () => {
    expect(matchConnections("2", connections).map((c) => c.place)).toEqual([2])
    expect(matchConnections("x", connections)).toEqual([])
    expect(choiceTitle(connections[1])).toBe("2 · http")
  })
})

describe("connectionJSON", () => {
  test("writes a connection as the connection format takes it, without empty params or values", () => {
    expect(JSON.parse(connectionJSON({ channel: "http", kind: "reverse-call", params: { url: "https://a" }, values: {} }))).toEqual({
      channel: "http",
      kind: "reverse-call",
      params: { url: "https://a" },
    })
  })
})

// Types for the parts of Apache Guacamole's JavaScript client the web app
// uses (guacamole-common.js beside this file, 1.6.0): a client over a
// WebSocket tunnel, its display, mouse and keyboard, and the streams of its
// clipboard and of the values a server asks for; a session recording's
// player, over a tunnel of the web app's own, and its key events.

declare namespace Guacamole {
  class Status {
    constructor(code: number, message?: string)
    code: number
    message?: string
    isError(): boolean
  }

  // The streams a client opens (OutputStream) and the server's
  // (InputStream): a clipboard's, an argument value's.
  class InputStream {
    index: number
  }
  class OutputStream {
    index: number
  }

  // A tunnel: the base's state and handlers; one of the web app's own sets
  // connect, disconnect and sendMessage.
  class Tunnel {
    state: number
    onerror: ((status: Status) => void) | null
    onstatechange: ((state: number) => void) | null
    oninstruction: ((opcode: string, args: string[]) => void) | null
    connect: (data?: string) => void
    disconnect: () => void
    sendMessage: (...elements: unknown[]) => void
    setState(state: number): void
  }

  namespace Tunnel {
    const State: {
      CONNECTING: 0
      OPEN: 1
      CLOSED: 2
      UNSTABLE: 3
    }
  }

  // The protocol's parser: text in, as it comes, whole instructions out.
  class Parser {
    receive(text: string): void
    oninstruction: ((opcode: string, args: string[]) => void) | null
  }

  class WebSocketTunnel extends Tunnel {
    // The socket's URL: the client's connect data goes after a "?".
    constructor(tunnelURL: string)
  }

  class Display {
    getElement(): HTMLDivElement
    getWidth(): number
    getHeight(): number
    getScale(): number
    scale(scale: number): void
    showCursor(shown: boolean): void
    onresize: ((width: number, height: number) => void) | null
  }

  class Client {
    constructor(tunnel: Tunnel)
    // data: the connect's query, as the tunnel's URL takes it.
    connect(data?: string): void
    disconnect(): void
    getDisplay(): Display
    sendSize(width: number, height: number): void
    sendKeyEvent(pressed: 0 | 1 | boolean, keysym: number): void
    sendMouseState(state: Mouse.State, applyDisplayScale?: boolean): void
    createClipboardStream(mimetype: string): OutputStream
    createArgumentValueStream(mimetype: string, name: string): OutputStream
    // States: Client.State.
    onstatechange: ((state: number) => void) | null
    onerror: ((status: Status) => void) | null
    onclipboard: ((stream: InputStream, mimetype: string) => void) | null
    // The server asks for connection params it lacks (a login): their
    // names (username, password, domain, ...).
    onrequired: ((parameters: string[]) => void) | null
    onname: ((name: string) => void) | null
  }

  namespace Client {
    const State: {
      IDLE: 0
      CONNECTING: 1
      WAITING: 2
      CONNECTED: 3
      DISCONNECTING: 4
      DISCONNECTED: 5
    }
  }

  class Mouse {
    constructor(element: Element)
    onEach(types: string[], listener: (event: Mouse.Event) => void): void
  }

  namespace Mouse {
    class State {
      x: number
      y: number
      left: boolean
      middle: boolean
      right: boolean
      up: boolean
      down: boolean
    }
    class Event {
      state: State
    }
  }

  class Keyboard {
    constructor(element?: Element | Document)
    // false: the browser does not get the key.
    onkeydown: ((keysym: number) => boolean | void) | null
    onkeyup: ((keysym: number) => void) | null
    reset(): void
  }

  class StringReader {
    constructor(stream: InputStream)
    ontext: ((text: string) => void) | null
    onend: (() => void) | null
  }

  class StringWriter {
    constructor(stream: OutputStream)
    sendText(text: string): void
    sendEnd(): void
  }

  // A session recording's player (guacd's format): fed by a tunnel, its
  // frames playable as they come; times are milliseconds from its first.
  class SessionRecording {
    // refreshInterval: how often onseek says the position while it plays.
    constructor(source: Tunnel | Blob, refreshInterval?: number)
    connect(data?: string): void
    disconnect(): void
    abort(): void
    getDisplay(): Display
    isPlaying(): boolean
    getPosition(): number
    getDuration(): number
    play(): void
    pause(): void
    seek(position: number, callback?: () => void): void
    cancel(): void
    onload: (() => void) | null
    onerror: ((message: string) => void) | null
    onabort: (() => void) | null
    // duration: what is read so far; parsedSize: its characters.
    onprogress: ((duration: number, parsedSize: number) => void) | null
    onplay: (() => void) | null
    onpause: (() => void) | null
    onseek: ((position: number) => void) | null
    // Once its tunnel closed: every key event in it.
    onkeyevents: ((events: KeyEventInterpreter.KeyEvent[]) => void) | null
  }

  // Reads a recording's key instructions as its player does, each key
  // named by guacamole-common's own keysym table: what a recording made a
  // video kept of its keys is read with it.
  class KeyEventInterpreter {
    // startTimestamp: the recording's first sync's, its 0.
    constructor(startTimestamp?: number)
    // A key instruction's args: keysym, pressed, timestamp.
    handleKeyEvent(args: readonly string[]): void
    getEvents(): KeyEventInterpreter.KeyEvent[]
  }

  namespace KeyEventInterpreter {
    class KeyDefinition {
      keysym: number
      name: string
      // What it types, for a key that types something.
      value?: string
    }
    class KeyEvent {
      definition: KeyDefinition
      pressed: boolean
      // Milliseconds from the recording's first frame.
      timestamp: number
    }
  }

  const API_VERSION: string
}

export default Guacamole

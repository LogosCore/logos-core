// Types for the parts of Apache Guacamole's JavaScript client the web app
// uses (guacamole-common.js beside this file, 1.6.0): a client over a
// WebSocket tunnel, its display, mouse and keyboard, and the streams of its
// clipboard and of the values a server asks for.

declare namespace Guacamole {
  class Status {
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

  class Tunnel {
    onerror: ((status: Status) => void) | null
    onstatechange: ((state: number) => void) | null
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

  const API_VERSION: string
}

export default Guacamole

// Package desktops holds a desktop's settings (docs:
// contracts/minion-messages/tunnels.md, Desktops): what an operator picks
// each time a desktop opens in the web app, by its front, and the guacd
// params they come to. The task keeps what the tunnel is (its server, its
// login); a setting is how one session draws it, so each session opened
// draws it its own way, and a change needs no new tunnel.
package desktops

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	// The time zones a setting is checked against (Timezone), whatever
	// core's host has.
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// The settings' keys.
const (
	// Every desktop's: its clipboard, and whether it takes input.
	Clipboard = "clipboard"
	ViewOnly  = "view_only"
	// RDP's and VNC's: what the picture costs.
	Quality = "quality"
	// RDP's.
	Keyboard    = "keyboard"
	Screen      = "screen"
	ClientOS    = "client_os"
	Console     = "console"
	Program     = "program"
	RemoteApp   = "remote_app"
	Security    = "security"
	IgnoreCert  = "ignore_cert"
	Workstation = "workstation_name"
	ClientName  = "client_name"
	// RDP's and SSH's: the time zone the server is told.
	Timezone = "timezone"
	// VNC's.
	ClipboardEncoding = "clipboard_encoding"
	Cursor            = "cursor"
	// A terminal's: SSH's and Telnet's.
	ColorScheme = "color_scheme"
	FontSize    = "font_size"
	Scrollback  = "scrollback"
	// SSH's.
	Command   = "command"
	Keepalive = "keepalive"
	HostKey   = "host_key"
	// Telnet's: its login's prompts.
	UsernamePrompt = "username_prompt"
	PasswordPrompt = "password_prompt"
	LoginSuccess   = "login_success"
	LoginFailure   = "login_failure"
)

// The clipboard's ways (Clipboard): both, the desktop's copies to the
// operator (out), the operator's text into the desktop (in), or neither.
const (
	ClipboardBoth = "both"
	ClipboardOut  = "out"
	ClipboardIn   = "in"
	ClipboardOff  = "off"
)

// Bounds of the text settings: the web app sends them in its socket's URL.
const (
	maxProgram = 1024
	maxCommand = 2048
	maxHostKey = 2048
	maxPrompt  = 256
	// A NetBIOS name's (Workstation, ClientName): RDP's client name is cut
	// there too.
	maxComputerName = 15
	maxTimezone     = 64
)

// Fields are a desktop front's settings, the form's order, each with its
// default; what is seldom changed is Advanced. None for a front that is no
// desktop's.
func Fields(front string) []protocol.FieldSpec {
	var fields []protocol.FieldSpec
	switch front {
	case protocol.FrontRDP:
		fields = []protocol.FieldSpec{quality(front), keyboard(), clipboard(), viewOnly(), screen(),
			{Key: Workstation, Type: protocol.FieldString, Label: "Workstation name", MaxLength: maxComputerName,
				Placeholder: "DESKTOP-4F7Q2KD",
				Description: "The computer name the server's logon events show. None: the client name, or a DESKTOP-XXXXXXX of this tunnel's own."},
			{Key: ClientName, Type: protocol.FieldString, Label: "Client name", MaxLength: maxComputerName,
				Placeholder: "DESKTOP-4F7Q2KD",
				Description: "The computer name the server's session shows (CLIENTNAME). None: the workstation name, or a DESKTOP-XXXXXXX of this tunnel's own."},
			{Key: ClientOS, Type: protocol.FieldEnum, Label: "Client OS", Default: "windows",
				Description: "The operating system the server is told the client runs (its logs show it).",
				Options: []protocol.EnumOption{
					{Value: "windows", Label: "Windows", Description: "As Windows' own client (mstsc) says."},
					{Value: "linux", Label: "Linux", Description: "As FreeRDP's on Linux (xfreerdp) says."},
					{Value: "none", Label: "Not said", Description: "guacd's own: no client does this but Guacamole."}}},
			timezone(front),
			{Key: Console, Type: protocol.FieldBool, Label: "Console session", Default: false, Advanced: true,
				Description: "The server's console (admin) session, as mstsc /admin opens it: on a server, one that needs no Remote Desktop licence."},
			{Key: Program, Type: protocol.FieldString, Label: "Program", MaxLength: maxProgram, Advanced: true,
				Description: `A program the session runs instead of the desktop (its path), where the server allows one: an RDS host's policy. None: the desktop.`},
			{Key: RemoteApp, Type: protocol.FieldString, Label: "RemoteApp", MaxLength: maxProgram, Advanced: true,
				Placeholder: "||notepad",
				Description: `One application shown alone, as the server publishes it (its alias after two bars: ||notepad). None: the desktop.`},
			{Key: Security, Type: protocol.FieldEnum, Label: "Security", Default: "any", Advanced: true,
				Description: "The security the session negotiates.",
				Options: []protocol.EnumOption{
					{Value: "any", Label: "Any", Description: "Whatever the server takes."},
					{Value: "nla", Label: "NLA", Description: "Network level authentication: the login before the session."},
					{Value: "nla-ext", Label: "Extended NLA", Description: "NLA, its extended form."},
					{Value: "tls", Label: "TLS", Description: "TLS, the login in the session."},
					{Value: "rdp", Label: "RDP encryption", Description: "RDP's own encryption: old servers."}}},
			{Key: IgnoreCert, Type: protocol.FieldBool, Label: "Take any certificate", Default: true, Advanced: true,
				Description: "Take a server certificate that cannot be checked (a self-signed one, as most RDP servers have)."}}
	case protocol.FrontVNC:
		fields = []protocol.FieldSpec{quality(front), clipboard(), viewOnly(),
			{Key: ClipboardEncoding, Type: protocol.FieldEnum, Label: "Clipboard encoding", Default: "ISO8859-1", Advanced: true,
				Description: "How the server's clipboard text is encoded: VNC's own is Latin-1, which has no Cyrillic, say.",
				Options: []protocol.EnumOption{
					{Value: "ISO8859-1", Label: "Latin-1", Description: "VNC's own."},
					{Value: "UTF-8", Label: "UTF-8"},
					{Value: "UTF-16", Label: "UTF-16"},
					{Value: "CP1252", Label: "Windows-1252"}}},
			{Key: Cursor, Type: protocol.FieldEnum, Label: "Cursor", Default: "local", Advanced: true,
				Description: "Where the mouse cursor is drawn.",
				Options: []protocol.EnumOption{
					{Value: "local", Label: "Here", Description: "By your browser: it follows the mouse at once."},
					{Value: "remote", Label: "By the server", Description: "In the server's picture, as its screen shows it."}}}}
	case protocol.FrontSSH:
		fields = []protocol.FieldSpec{
			{Key: Command, Type: protocol.FieldString, Label: "Command", MaxLength: maxCommand,
				Description: "A command the server runs instead of a shell: the session ends with it. None: the login's shell."},
			colorScheme(), fontSize(), clipboard(), viewOnly(), timezone(front), scrollback(),
			{Key: Keepalive, Type: protocol.FieldDuration, Label: "Keepalive", Default: "0s", Min: "0s", Max: "1h", Advanced: true,
				Description: "How often an idle session tells the server it is there, so that no firewall drops it: 0s never."},
			{Key: HostKey, Type: protocol.FieldString, Label: "Host key", MaxLength: maxHostKey, Advanced: true,
				Placeholder: "ssh-ed25519 AAAA…",
				Description: "The server's public key, as ssh-keyscan or a known_hosts line gives it: the session opens only if the server holds it. None: not checked."}}
	case protocol.FrontTelnet:
		fields = []protocol.FieldSpec{colorScheme(), fontSize(), clipboard(), viewOnly(), scrollback(),
			prompt(UsernamePrompt, "Username prompt", "[Ll]ogin:",
				"What the server asks a username with, as a regular expression (POSIX, any case): the login's username is typed after it."),
			prompt(PasswordPrompt, "Password prompt", "[Pp]assword:",
				"What the server asks a password with: the login's password is typed after it."),
			prompt(LoginSuccess, "Login success", "",
				"What the server says once the login worked: the terminal shows nothing until then. None: it shows at once."),
			prompt(LoginFailure, "Login failure", "",
				"What the server says when the login failed: the session ends there, saying so.")}
	default:
		return nil
	}
	for i := range fields {
		fields[i].Order = i + 1
	}
	return fields
}

func quality(front string) protocol.FieldSpec {
	f := protocol.FieldSpec{Key: Quality, Type: protocol.FieldEnum, Label: "Quality", Default: "balanced",
		Description: "What the picture costs: the tunnel carries every change of it."}
	if front == protocol.FrontRDP {
		f.Options = []protocol.EnumOption{
			{Value: "speed", Label: "Speed", Description: "16-bit color, RDP's older drawing and no effects: for a slow tunnel."},
			{Value: "balanced", Label: "Balanced", Description: "Full color and smoothed text, no wallpaper or animations; what changes often may reach you as JPEG."},
			{Value: "best", Label: "Best", Description: "Every effect (wallpaper, themes, animations), and lossless all the way: for a fast network."}}
	} else {
		f.Options = []protocol.EnumOption{
			{Value: "speed", Label: "Speed", Description: "JPEG at a low quality and the most compression, where the server speaks Tight: for a slow tunnel."},
			{Value: "balanced", Label: "Balanced", Description: "Lossless from the server; what changes often may reach you as JPEG."},
			{Value: "best", Label: "Best", Description: "Lossless all the way: for a fast network."}}
	}
	return f
}

// The quality presets' guacd params (Quality). RDP's speed leaves the
// graphics pipeline, which takes 32-bit color alone, for 16-bit color;
// VNC's asks a Tight server for JPEG, which guacd's encodings otherwise
// leave out.
var (
	rdpQuality = map[string]map[string]string{
		"speed":    {"disable-gfx": "true", "color-depth": "16"},
		"balanced": {"enable-font-smoothing": "true"},
		"best": {"enable-font-smoothing": "true", "enable-wallpaper": "true", "enable-theming": "true",
			"enable-desktop-composition": "true", "enable-full-window-drag": "true", "enable-menu-animations": "true",
			"force-lossless": "true"},
	}
	vncQuality = map[string]map[string]string{
		"speed":    {"encodings": "tight zrle ultra copyrect hextile zlib corre rre raw", "quality-level": "3", "compress-level": "9"},
		"balanced": {},
		"best":     {"force-lossless": "true"},
	}
)

// keyboardLayouts are guacd's RDP keymaps (server-layout), the default
// first.
var keyboardLayouts = []protocol.EnumOption{
	{Value: "en-us-qwerty", Label: "English (US)"},
	{Value: "en-gb-qwerty", Label: "English (UK)"},
	{Value: "de-de-qwertz", Label: "German"},
	{Value: "de-ch-qwertz", Label: "German (Switzerland)"},
	{Value: "fr-fr-azerty", Label: "French"},
	{Value: "fr-be-azerty", Label: "French (Belgium)"},
	{Value: "fr-ca-qwerty", Label: "French (Canada)"},
	{Value: "fr-ch-qwertz", Label: "French (Switzerland)"},
	{Value: "es-es-qwerty", Label: "Spanish"},
	{Value: "es-latam-qwerty", Label: "Spanish (Latin America)"},
	{Value: "it-it-qwerty", Label: "Italian"},
	{Value: "pt-pt-qwerty", Label: "Portuguese"},
	{Value: "pt-br-qwerty", Label: "Portuguese (Brazil)"},
	{Value: "cs-cz-qwertz", Label: "Czech"},
	{Value: "da-dk-qwerty", Label: "Danish"},
	{Value: "hu-hu-qwertz", Label: "Hungarian"},
	{Value: "ja-jp-qwerty", Label: "Japanese"},
	{Value: "no-no-qwerty", Label: "Norwegian"},
	{Value: "pl-pl-qwerty", Label: "Polish (programmers)"},
	{Value: "ro-ro-qwerty", Label: "Romanian"},
	{Value: "sv-se-qwerty", Label: "Swedish"},
	{Value: "tr-tr-qwerty", Label: "Turkish (Q)"},
	{Value: "failsafe", Label: "Failsafe",
		Description: "Each character as itself, whatever the server's layout (Ukrainian, Russian: none of the others'); shortcuts with letters may not work."},
}

func keyboard() protocol.FieldSpec {
	return protocol.FieldSpec{Key: Keyboard, Type: protocol.FieldEnum, Label: "Keyboard layout", Default: keyboardLayouts[0].Value,
		Options:     append([]protocol.EnumOption(nil), keyboardLayouts...),
		Description: "The server's layout: guacd presses the keys that make what you type there. A character none of its keys makes goes as itself."}
}

func clipboard() protocol.FieldSpec {
	return protocol.FieldSpec{Key: Clipboard, Type: protocol.FieldEnum, Label: "Clipboard", Default: ClipboardBoth,
		Description: "Which way text goes between the desktop's clipboard and yours.",
		Options: []protocol.EnumOption{
			{Value: ClipboardBoth, Label: "Both ways"},
			{Value: ClipboardOut, Label: "From the desktop", Description: "What it copies reaches you; nothing of yours reaches it."},
			{Value: ClipboardIn, Label: "Into the desktop", Description: "Your text reaches it; nothing it copies reaches you."},
			{Value: ClipboardOff, Label: "Off"}}}
}

func viewOnly() protocol.FieldSpec {
	return protocol.FieldSpec{Key: ViewOnly, Type: protocol.FieldBool, Label: "View only", Default: false,
		Description: "Watch it: no key or click of yours reaches the server."}
}

// screenSizes are the fixed sizes a desktop opens at (Screen), common
// monitors' first: the default one, then by how many have it.
var screenSizes = [][2]int{{1920, 1080}, {1366, 768}, {1536, 864}, {1440, 900}, {1280, 720}, {1600, 900},
	{1280, 1024}, {1680, 1050}, {1920, 1200}, {2560, 1440}, {3840, 2160}}

// The screen's sizes that follow the pane (Screen).
const (
	ScreenPane          = "pane"
	ScreenPaneReconnect = "pane-reconnect"
)

func screen() protocol.FieldSpec {
	options := make([]protocol.EnumOption, 0, len(screenSizes)+2)
	for _, wh := range screenSizes {
		options = append(options, protocol.EnumOption{Value: fmt.Sprintf("%dx%d", wh[0], wh[1]), Label: fmt.Sprintf("%d × %d", wh[0], wh[1])})
	}
	options = append(options,
		protocol.EnumOption{Value: ScreenPane, Label: "Follow the pane",
			Description: "The pane's size, which the server changes as the pane does: its logs show each change."},
		protocol.EnumOption{Value: ScreenPaneReconnect, Label: "Follow the pane, reconnecting",
			Description: "The same, the session reconnecting at each new size: for a server that cannot change it live (older Windows, some xrdp)."})
	return protocol.FieldSpec{Key: Screen, Type: protocol.FieldEnum, Label: "Screen size", Default: options[0].Value, Options: options,
		Description: "The desktop's size, as a monitor's: the pane scales it."}
}

// clientOS are the client operating systems' osMajorType and osMinorType
// (ClientOS), as MS-RDPBCGR numbers them.
var clientOS = map[string][2]string{"windows": {"1", "3"}, "linux": {"4", "7"}, "none": {"0", "0"}}

func timezone(front string) protocol.FieldSpec {
	description := "The time zone the server is told the client is in (its logs show it): an IANA name. None: UTC."
	if front == protocol.FrontSSH {
		description = "The session's TZ, where the server takes it (AcceptEnv TZ): an IANA name. None: UTC."
	}
	return protocol.FieldSpec{Key: Timezone, Type: protocol.FieldString, Label: "Time zone", MaxLength: maxTimezone,
		Format: protocol.FormatTimezone, Placeholder: "UTC", Description: description}
}

func colorScheme() protocol.FieldSpec {
	return protocol.FieldSpec{Key: ColorScheme, Type: protocol.FieldEnum, Label: "Colors", Default: "gray-black",
		Description: "The terminal's text and background.",
		Options: []protocol.EnumOption{
			{Value: "gray-black", Label: "Gray on black"},
			{Value: "white-black", Label: "White on black"},
			{Value: "green-black", Label: "Green on black"},
			{Value: "black-white", Label: "Black on white"}}}
}

func fontSize() protocol.FieldSpec {
	return protocol.FieldSpec{Key: FontSize, Type: protocol.FieldInt, Label: "Font size", Default: 12, Min: 6, Max: 48, Unit: "pt",
		Description: "The terminal's text, in points."}
}

func scrollback() protocol.FieldSpec {
	return protocol.FieldSpec{Key: Scrollback, Type: protocol.FieldInt, Label: "Scrollback", Default: 1000, Min: 0, Max: 10000,
		Unit: "rows", Advanced: true, Description: "The rows the terminal keeps above its screen to scroll back to."}
}

func prompt(key, label, placeholder, description string) protocol.FieldSpec {
	return protocol.FieldSpec{Key: key, Type: protocol.FieldString, Label: label, MaxLength: maxPrompt, Advanced: true,
		Placeholder: placeholder, Description: description}
}

// Params are the guacd params a desktop's settings come to: values by
// key, as JSON gives them, one left out taking its default, for a
// session whose server guacd reaches at host:port (core's port for the
// task: an SSH host key is checked as that address's). Problems come by
// key, each setting's, as protocol.TaskArgs says them; with any, params
// is nil. A front that is no desktop's has none: a value is a problem.
func Params(front string, values map[string]any, host string, port int) (map[string]string, map[string]string) {
	fields := Fields(front)
	args, problems := protocol.TaskArgs(fields, values)
	if problems != nil {
		return nil, problems
	}
	p := map[string]string{}
	if len(fields) == 0 {
		return p, nil
	}
	a := settingArgs{args: args, params: p, wrong: map[string]string{}}
	a.everyDesktop()
	switch front {
	case protocol.FrontRDP:
		a.rdp()
		a.timezone()
	case protocol.FrontVNC:
		a.vnc()
	case protocol.FrontSSH:
		a.terminal()
		a.timezone()
		a.ssh(host, port)
	case protocol.FrontTelnet:
		a.terminal()
		a.telnet()
	}
	if len(a.wrong) > 0 {
		return nil, a.wrong
	}
	return p, nil
}

// settingArgs are a desktop's settings as protocol.TaskArgs read them
// (each its field's type, its default where none was given), the guacd
// params they come to, and what is wrong with them, by key.
type settingArgs struct {
	args   map[string]any
	params map[string]string
	wrong  map[string]string
}

func (a settingArgs) str(key string) string {
	s, _ := protocol.ArgString(a.args, key)
	return strings.TrimSpace(s)
}

func (a settingArgs) on(key string) bool {
	b, _ := protocol.ArgBool(a.args, key)
	return b
}

func (a settingArgs) num(key string) string {
	n, _ := protocol.ArgInt(a.args, key)
	return strconv.FormatInt(n, 10)
}

// set gives param a value: none, it is not sent.
func (a settingArgs) set(param, value string) {
	if value != "" {
		a.params[param] = value
	}
}

// presets: the params of a preset's value (Quality).
func (a settingArgs) presets(key string, by map[string]map[string]string) {
	for k, v := range by[a.str(key)] {
		a.params[k] = v
	}
}

// everyDesktop: its clipboard's ways, and whether it takes input.
func (a settingArgs) everyDesktop() {
	switch a.str(Clipboard) {
	case ClipboardOut:
		a.params["disable-paste"] = "true"
	case ClipboardIn:
		a.params["disable-copy"] = "true"
	case ClipboardOff:
		a.params["disable-copy"], a.params["disable-paste"] = "true", "true"
	}
	if a.on(ViewOnly) {
		a.params["read-only"] = "true"
	}
}

func (a settingArgs) rdp() {
	a.presets(Quality, rdpQuality)
	a.params["server-layout"] = a.str(Keyboard)
	switch size := a.str(Screen); size {
	case ScreenPane:
		a.params["resize-method"] = "display-update"
	case ScreenPaneReconnect:
		a.params["resize-method"] = "reconnect"
	default:
		// A monitor's, at its usual density, whatever the pane is.
		w, h, _ := strings.Cut(size, "x")
		a.params["width"], a.params["height"], a.params["dpi"] = w, h, "96"
	}
	osType := clientOS[a.str(ClientOS)]
	a.params["os-major-type"], a.params["os-minor-type"] = osType[0], osType[1]
	if a.on(Console) {
		a.params["console"] = "true"
	}
	a.set("initial-program", a.str(Program))
	a.set("remote-app", a.str(RemoteApp))
	for key, param := range map[string]string{Workstation: "workstation-name", ClientName: "client-name"} {
		name := a.str(key)
		if name == "" {
			continue
		}
		if !computerName.MatchString(name) || strings.Trim(name, "0123456789") == "" {
			a.wrong[key] = "a computer name: letters, digits and hyphens, not digits alone"
			continue
		}
		a.params[param] = strings.ToUpper(name)
	}
	a.params["security"] = a.str(Security)
	a.params["ignore-cert"] = strconv.FormatBool(a.on(IgnoreCert))
}

// vnc: never the server's screen size. guacd otherwise asks a server that
// can resize (ExtendedDesktopSize: TigerVNC, RealVNC's virtual mode) to
// take the web app's pane size the moment it connects: the console's
// resolution changes under whoever sits at it. The pane scales the
// server's screen instead.
func (a settingArgs) vnc() {
	a.params["disable-display-resize"] = "true"
	a.presets(Quality, vncQuality)
	a.params["clipboard-encoding"] = a.str(ClipboardEncoding)
	a.params["cursor"] = a.str(Cursor)
}

// terminal: SSH's and Telnet's appearance.
func (a settingArgs) terminal() {
	a.params["color-scheme"] = a.str(ColorScheme)
	a.params["font-size"] = a.num(FontSize)
	a.params["scrollback"] = a.num(Scrollback)
}

// timezone: RDP's and SSH's; none, UTC, never the operator's own.
func (a settingArgs) timezone() {
	tz := a.str(Timezone)
	if tz == "" {
		tz = "UTC"
	}
	if err := checkTimezone(tz); err != nil {
		a.wrong[Timezone] = err.Error()
		return
	}
	a.params["timezone"] = tz
}

func (a settingArgs) ssh(host string, port int) {
	a.set("command", a.str(Command))
	if ms, _ := protocol.ArgInt(a.args, Keepalive); ms > 0 {
		// Whole seconds, up: libssh2 counts no less than 2.
		a.params["server-alive-interval"] = strconv.FormatInt((ms+999)/1000, 10)
	}
	if key := a.str(HostKey); key != "" {
		line, err := knownHost(key, host, port)
		if err != nil {
			a.wrong[HostKey] = err.Error()
			return
		}
		a.params["host-key"] = line
	}
}

// telnet: its login's prompts, guacd's POSIX extended regular
// expressions: Go's syntax is close enough to catch a mistyped one.
func (a settingArgs) telnet() {
	for key, param := range map[string]string{UsernamePrompt: "username-regex", PasswordPrompt: "password-regex",
		LoginSuccess: "login-success-regex", LoginFailure: "login-failure-regex"} {
		pattern := a.str(key)
		if pattern == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			a.wrong[key] = "not a regular expression: " + strings.TrimPrefix(err.Error(), "error parsing regexp: ")
			continue
		}
		a.params[param] = pattern
	}
}

// ComputerNames fills an RDP desktop's computer names that its settings
// left out (params: Params'), as one computer has one name: its workstation
// name (NTLM's) the client name, its client name the workstation name, and
// with neither, both the task's own (ComputerName).
func ComputerNames(task uuid.UUID, params map[string]string) {
	ws, client := params["workstation-name"], params["client-name"]
	switch {
	case ws == "" && client == "":
		ws = ComputerName(task)
		client = ws
	case ws == "":
		ws = client
	case client == "":
		client = ws
	}
	params["workstation-name"], params["client-name"] = ws, client
}

// ComputerName is the computer name of a task's RDP desktops when their
// settings give none (ComputerNames): as Windows names a new computer,
// DESKTOP- and seven capitals and digits, drawn from the task's id, so
// that every desktop of a tunnel is the same computer to its server.
func ComputerName(task uuid.UUID) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	sum := sha256.Sum256(append([]byte("logos-workstation:"), task[:]...))
	name := []byte("DESKTOP-0000000")
	for i := range 7 {
		name[8+i] = chars[int(sum[i])%len(chars)]
	}
	return string(name)
}

// checkTimezone: a time zone as IANA names it (Europe/Berlin, UTC), one
// guacd's time zones have too: Go's own names, but for its "Local".
func checkTimezone(tz string) error {
	if tz == "Local" || strings.ContainsAny(tz, " ,") {
		return errors.New("a time zone as IANA names it: Europe/Berlin, America/New_York, UTC")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return errors.New("no such time zone: an IANA name, such as Europe/Berlin, America/New_York, UTC")
	}
	return nil
}

// computerName is a computer name as Windows takes one (Workstation,
// ClientName), its length the field's.
var computerName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// knownHost is an SSH server's host key as guacd checks it: a known_hosts
// line for the address guacd connects to (host:port, core's), which
// libssh2 matches the server's key under. key is the key's type and
// base64 (ssh-ed25519 AAAA…), as ssh-keyscan, a known_hosts line or a
// public key file says it: a host before them, a comment after, go.
func knownHost(key, host string, port int) (string, error) {
	fields := strings.Fields(key)
	for i, f := range fields {
		if !keyType(f) || i+1 >= len(fields) {
			continue
		}
		if b, err := base64.StdEncoding.DecodeString(fields[i+1]); err != nil || len(b) == 0 {
			return "", errors.New("the key after its type is no base64")
		}
		// As known_hosts writes it: a host alone at port 22.
		addr := strings.Trim(host, "[]")
		if port != 22 {
			addr = "[" + addr + "]:" + strconv.Itoa(port)
		}
		return fmt.Sprintf("%s %s %s", addr, f, fields[i+1]), nil
	}
	return "", errors.New("a public key: its type and key, as ssh-keyscan gives them (ssh-ed25519 AAAA…)")
}

// keyType: an SSH public key's type, as OpenSSH names them.
func keyType(s string) bool {
	for _, prefix := range []string{"ssh-", "ecdsa-sha2-", "sk-ssh-", "sk-ecdsa-sha2-"} {
		if strings.HasPrefix(s, prefix) && len(s) > len(prefix) {
			return true
		}
	}
	return false
}

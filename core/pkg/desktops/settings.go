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
	Resize      = "resize"
	Console     = "console"
	Program     = "program"
	RemoteApp   = "remote_app"
	Security    = "security"
	IgnoreCert  = "ignore_cert"
	Workstation = "workstation_name"
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
	// A NetBIOS name's (Workstation).
	maxWorkstation = 15
)

// Fields are a desktop front's settings, the form's order, each with its
// default; what is seldom changed is Advanced. None for a front that is no
// desktop's.
func Fields(front string) []protocol.FieldSpec {
	var fields []protocol.FieldSpec
	switch front {
	case protocol.FrontRDP:
		fields = []protocol.FieldSpec{quality(front), keyboard(), clipboard(), viewOnly(),
			{Key: Workstation, Type: protocol.FieldString, Label: "Workstation name", MaxLength: maxWorkstation,
				Placeholder: "DESKTOP-4F7Q2KD",
				Description: "The computer name the server's logon events show. None: a DESKTOP-XXXXXXX of this tunnel's own."},
			{Key: Resize, Type: protocol.FieldEnum, Label: "Resize", Default: "follow", Advanced: true,
				Description: "What the session does as the desktop's pane changes size.",
				Options: []protocol.EnumOption{
					{Value: "follow", Label: "Follow the pane", Description: "The server changes the desktop's size as it goes."},
					{Value: "reconnect", Label: "Reconnect", Description: "The session reconnects at the new size: for a server that cannot change it live (older Windows, some xrdp)."},
					{Value: "fixed", Label: "Fixed", Description: "The size it opened with: the pane scales it."}}},
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
			colorScheme(), fontSize(), clipboard(), viewOnly(), scrollback(),
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
// key, as protocol.TaskArgs says them; with any, params is nil. A front
// that is no desktop's has none: a value is a problem.
func Params(front string, values map[string]any, host string, port int) (map[string]string, map[string]string) {
	fields := Fields(front)
	args, problems := protocol.TaskArgs(fields, values)
	if problems != nil {
		return nil, problems
	}
	str := func(key string) string {
		s, _ := protocol.ArgString(args, key)
		return strings.TrimSpace(s)
	}
	on := func(key string) bool {
		b, _ := protocol.ArgBool(args, key)
		return b
	}
	num := func(key string) string {
		n, _ := protocol.ArgInt(args, key)
		return strconv.FormatInt(n, 10)
	}
	p := map[string]string{}
	set := func(param, value string) {
		if value != "" {
			p[param] = value
		}
	}
	if len(fields) == 0 {
		return p, nil
	}
	switch str(Clipboard) {
	case ClipboardOut:
		p["disable-paste"] = "true"
	case ClipboardIn:
		p["disable-copy"] = "true"
	case ClipboardOff:
		p["disable-copy"], p["disable-paste"] = "true", "true"
	}
	if on(ViewOnly) {
		p["read-only"] = "true"
	}
	switch front {
	case protocol.FrontRDP:
		for k, v := range rdpQuality[str(Quality)] {
			p[k] = v
		}
		p["server-layout"] = str(Keyboard)
		switch str(Resize) {
		case "follow":
			p["resize-method"] = "display-update"
		case "reconnect":
			p["resize-method"] = "reconnect"
		}
		if on(Console) {
			p["console"] = "true"
		}
		set("initial-program", str(Program))
		set("remote-app", str(RemoteApp))
		if name := str(Workstation); name != "" {
			if !workstationName.MatchString(name) || strings.Trim(name, "0123456789") == "" {
				return nil, map[string]string{Workstation: "a computer name: letters, digits and hyphens, not digits alone"}
			}
			p["workstation-name"] = strings.ToUpper(name)
		}
		p["security"] = str(Security)
		p["ignore-cert"] = strconv.FormatBool(on(IgnoreCert))
	case protocol.FrontVNC:
		for k, v := range vncQuality[str(Quality)] {
			p[k] = v
		}
		p["clipboard-encoding"] = str(ClipboardEncoding)
		p["cursor"] = str(Cursor)
	case protocol.FrontSSH, protocol.FrontTelnet:
		p["color-scheme"] = str(ColorScheme)
		p["font-size"] = num(FontSize)
		p["scrollback"] = num(Scrollback)
	}
	switch front {
	case protocol.FrontSSH:
		set("command", str(Command))
		if ms, _ := protocol.ArgInt(args, Keepalive); ms > 0 {
			// Whole seconds, up: libssh2 counts no less than 2.
			p["server-alive-interval"] = strconv.FormatInt((ms+999)/1000, 10)
		}
		if key := str(HostKey); key != "" {
			line, err := knownHost(key, host, port)
			if err != nil {
				return nil, map[string]string{HostKey: err.Error()}
			}
			p["host-key"] = line
		}
	case protocol.FrontTelnet:
		var wrong map[string]string
		for key, param := range map[string]string{UsernamePrompt: "username-regex", PasswordPrompt: "password-regex",
			LoginSuccess: "login-success-regex", LoginFailure: "login-failure-regex"} {
			pattern := str(key)
			if pattern == "" {
				continue
			}
			// guacd's are POSIX extended: Go's syntax is close enough to
			// catch a mistyped one.
			if _, err := regexp.Compile(pattern); err != nil {
				if wrong == nil {
					wrong = map[string]string{}
				}
				wrong[key] = "not a regular expression: " + strings.TrimPrefix(err.Error(), "error parsing regexp: ")
				continue
			}
			p[param] = pattern
		}
		if wrong != nil {
			return nil, wrong
		}
	}
	return p, nil
}

// DefaultWorkstation is the workstation name of a task's RDP desktops when
// none is set (Workstation): as Windows names a new computer, DESKTOP- and
// seven capitals and digits, drawn from the task's id, so that every
// desktop of a tunnel is the same computer to its server.
func DefaultWorkstation(task uuid.UUID) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	sum := sha256.Sum256(append([]byte("logos-workstation:"), task[:]...))
	name := []byte("DESKTOP-0000000")
	for i := range 7 {
		name[8+i] = chars[int(sum[i])%len(chars)]
	}
	return string(name)
}

// workstationName is a computer name as Windows takes one (Workstation),
// its length the field's.
var workstationName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

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

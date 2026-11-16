package desktops

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A desktop front's settings, in the form's order, each with a default
// its type takes (the defaults alone make settings); none for another
// front.
func TestFields(t *testing.T) {
	want := map[string]string{
		protocol.FrontRDP:      "quality,keyboard,clipboard,view_only,screen,workstation_name,client_name,client_os,timezone,console,program,remote_app,security,ignore_cert",
		protocol.FrontVNC:      "quality,clipboard,view_only,clipboard_encoding,cursor",
		protocol.FrontSSH:      "command,color_scheme,font_size,clipboard,view_only,timezone,scrollback,keepalive,host_key",
		protocol.FrontTelnet:   "color_scheme,font_size,clipboard,view_only,scrollback,username_prompt,password_prompt,login_success,login_failure",
		protocol.FrontForward:  "",
		protocol.FrontTerminal: "",
		"":                     "",
	}
	for front, keys := range want {
		fields := Fields(front)
		var got []string
		for i, f := range fields {
			got = append(got, f.Key)
			if f.Order != i+1 || f.Label == "" || f.Description == "" {
				t.Errorf("%s %s: order %d, label %q, description %q", front, f.Key, f.Order, f.Label, f.Description)
			}
			if f.Type == protocol.FieldEnum {
				i := slices.IndexFunc(f.Options, func(o protocol.EnumOption) bool { return o.Value == f.Default })
				if i < 0 {
					t.Errorf("%s %s: default %v is no option", front, f.Key, f.Default)
				}
			}
		}
		if strings.Join(got, ",") != keys {
			t.Errorf("%s: %s, want %s", front, strings.Join(got, ","), keys)
		}
		if _, problems := protocol.TaskArgs(fields, map[string]any{}); problems != nil {
			t.Errorf("%s by its defaults: %v", front, problems)
		}
	}
	// The keyboard layouts are guacd 1.6.0's keymaps (src/protocols/rdp/keymaps).
	if n := len(keyboardLayouts); n != 23 || keyboardLayouts[0].Value != "en-us-qwerty" || keyboardLayouts[n-1].Value != "failsafe" {
		t.Errorf("layouts: %d", n)
	}
	// Each call its own: a caller changing one changes no other.
	a := Fields(protocol.FrontRDP)
	a[1].Options[0].Label = "changed"
	if Fields(protocol.FrontRDP)[1].Options[0].Label == "changed" {
		t.Error("Fields shares its options")
	}
}

// The defaults: guacd's but for smoothed text, a 1920x1080 screen, Windows
// said as the client's OS, UTC, and a self-signed certificate taken (RDP's),
// a terminal's appearance said, and UTC for SSH: nothing of the operator's
// own (their pane's size, their time zone).
func TestParamsByDefault(t *testing.T) {
	want := map[string]map[string]string{
		protocol.FrontRDP: {"enable-font-smoothing": "true", "server-layout": "en-us-qwerty", "width": "1920", "height": "1080",
			"dpi": "96", "os-major-type": "1", "os-minor-type": "3", "timezone": "UTC", "security": "any", "ignore-cert": "true"},
		protocol.FrontVNC:     {"clipboard-encoding": "ISO8859-1", "cursor": "local"},
		protocol.FrontSSH:     {"color-scheme": "gray-black", "font-size": "12", "scrollback": "1000", "timezone": "UTC"},
		protocol.FrontTelnet:  {"color-scheme": "gray-black", "font-size": "12", "scrollback": "1000"},
		protocol.FrontForward: {},
	}
	for front, w := range want {
		got, problems := Params(front, nil, "core", 41000)
		if problems != nil || !maps.Equal(got, w) {
			t.Errorf("%s: %v %v, want %v", front, got, problems, w)
		}
	}
}

// What an operator picks comes to guacd's params (numbers as JSON
// decodes them for the socket: json.Number).
func TestParamsChosen(t *testing.T) {
	const key = "AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	for _, c := range []struct {
		name   string
		front  string
		values map[string]any
		want   map[string]string
	}{
		{"rdp, every setting", protocol.FrontRDP, map[string]any{"quality": "speed", "keyboard": "de-de-qwertz", "clipboard": "out",
			"view_only": true, "screen": "1366x768", "client_os": "linux", "console": true, "program": ` C:\tools\run.exe `, "remote_app": "||notepad",
			"security": "nla", "ignore_cert": false, "workstation_name": "desktop-4f7q2kd", "client_name": "Laptop-07", "timezone": "Europe/Kyiv"},
			map[string]string{"disable-gfx": "true", "color-depth": "16", "server-layout": "de-de-qwertz", "disable-paste": "true",
				"read-only": "true", "width": "1366", "height": "768", "dpi": "96", "os-major-type": "4", "os-minor-type": "7", "console": "true", "initial-program": `C:\tools\run.exe`, "remote-app": "||notepad",
				"security": "nla", "ignore-cert": "false", "workstation-name": "DESKTOP-4F7Q2KD", "client-name": "LAPTOP-07", "timezone": "Europe/Kyiv"}},
		{"rdp at its best, following the pane, reconnecting", protocol.FrontRDP, map[string]any{"quality": "best", "screen": "pane-reconnect",
			"clipboard": "in", "client_os": "none"},
			map[string]string{"enable-font-smoothing": "true", "enable-wallpaper": "true", "enable-theming": "true",
				"enable-desktop-composition": "true", "enable-full-window-drag": "true", "enable-menu-animations": "true",
				"force-lossless": "true", "server-layout": "en-us-qwerty", "resize-method": "reconnect", "disable-copy": "true",
				"os-major-type": "0", "os-minor-type": "0", "timezone": "UTC", "security": "any", "ignore-cert": "true"}},
		{"rdp following the pane", protocol.FrontRDP, map[string]any{"screen": "pane"},
			map[string]string{"enable-font-smoothing": "true", "server-layout": "en-us-qwerty", "resize-method": "display-update",
				"os-major-type": "1", "os-minor-type": "3", "timezone": "UTC", "security": "any", "ignore-cert": "true"}},
		{"vnc for a slow tunnel", protocol.FrontVNC, map[string]any{"quality": "speed", "clipboard": "off", "clipboard_encoding": "UTF-8",
			"cursor": "remote"},
			map[string]string{"encodings": "tight zrle ultra copyrect hextile zlib corre rre raw", "quality-level": "3",
				"compress-level": "9", "disable-copy": "true", "disable-paste": "true", "clipboard-encoding": "UTF-8", "cursor": "remote"}},
		{"vnc at its best", protocol.FrontVNC, map[string]any{"quality": "best"},
			map[string]string{"force-lossless": "true", "clipboard-encoding": "ISO8859-1", "cursor": "local"}},
		{"ssh, a command, a keepalive and a host key", protocol.FrontSSH, map[string]any{"command": "top", "color_scheme": "black-white",
			"font_size": json.Number("16"), "scrollback": 5000, "keepalive": "1500ms", "host_key": "target.lan ssh-ed25519 " + key + " root@target", "timezone": "America/New_York"},
			map[string]string{"command": "top", "color-scheme": "black-white", "font-size": "16", "scrollback": "5000",
				"server-alive-interval": "2", "host-key": "[core]:41000 ssh-ed25519 " + key, "timezone": "America/New_York"}},
		{"telnet's prompts", protocol.FrontTelnet, map[string]any{"username_prompt": "User Name:", "password_prompt": "Password:",
			"login_success": "[>#]$", "login_failure": "% (Login invalid|Bad passwords)", "view_only": true},
			map[string]string{"color-scheme": "gray-black", "font-size": "12", "scrollback": "1000", "read-only": "true",
				"username-regex": "User Name:", "password-regex": "Password:", "login-success-regex": "[>#]$",
				"login-failure-regex": "% (Login invalid|Bad passwords)"}},
	} {
		got, problems := Params(c.front, c.values, "core", 41000)
		if problems != nil || !maps.Equal(got, c.want) {
			t.Errorf("%s: %v %v\nwant %v", c.name, got, problems, c.want)
		}
	}
	// A host key at port 22 names its host alone, as known_hosts does.
	if line, err := knownHost("ssh-ed25519 "+key, "10.0.0.5", 22); err != nil || line != "10.0.0.5 ssh-ed25519 "+key {
		t.Errorf("at 22: %q %v", line, err)
	}
	if line, err := knownHost("ecdsa-sha2-nistp256 "+key, "fd00::5", 41000); err != nil || line != "[fd00::5]:41000 ecdsa-sha2-nistp256 "+key {
		t.Errorf("an IPv6 host: %q %v", line, err)
	}
}

// A task's own computer name: Windows' kind, DESKTOP- and seven capitals
// and digits, a setting's own taken; the same for the task each time,
// another for another task.
func TestComputerName(t *testing.T) {
	task := uuid.New()
	name := ComputerName(task)
	if !regexp.MustCompile(`^DESKTOP-[A-Z0-9]{7}$`).MatchString(name) {
		t.Fatalf("%q", name)
	}
	if !computerName.MatchString(name) || len(name) > maxComputerName {
		t.Errorf("%q is no setting's", name)
	}
	if ComputerName(task) != name {
		t.Error("not the same for the task")
	}
	seen := map[string]bool{name: true}
	for range 100 {
		seen[ComputerName(uuid.New())] = true
	}
	if len(seen) < 100 {
		t.Errorf("%d names for 101 tasks", len(seen))
	}
}

// One computer, one name: a name left out is the other's, both the task's
// with neither, each its own when both are set.
func TestComputerNames(t *testing.T) {
	task := uuid.New()
	own := ComputerName(task)
	for _, c := range []struct{ ws, client, wantWS, wantClient string }{
		{"", "", own, own},
		{"WS01", "", "WS01", "WS01"},
		{"", "LAPTOP-07", "LAPTOP-07", "LAPTOP-07"},
		{"WS01", "LAPTOP-07", "WS01", "LAPTOP-07"},
	} {
		p := map[string]string{"hostname": "core"}
		if c.ws != "" {
			p["workstation-name"] = c.ws
		}
		if c.client != "" {
			p["client-name"] = c.client
		}
		ComputerNames(task, p)
		if p["workstation-name"] != c.wantWS || p["client-name"] != c.wantClient || p["hostname"] != "core" {
			t.Errorf("%q %q: %v", c.ws, c.client, p)
		}
	}
}

// A setting that is none, or not one of its values, says why by its key;
// a front that is no desktop's takes none.
func TestParamsProblems(t *testing.T) {
	for _, c := range []struct {
		front  string
		values map[string]any
		key    string
		reason string
	}{
		{protocol.FrontRDP, map[string]any{"color_depth": 8}, "color_depth", "not a declared field"},
		{protocol.FrontRDP, map[string]any{"quality": "ultra"}, "quality", "must be one of"},
		{protocol.FrontRDP, map[string]any{"security": "vmconnect"}, "security", "must be one of"},
		{protocol.FrontVNC, map[string]any{"command": "ls"}, "command", "not a declared field"},
		{protocol.FrontSSH, map[string]any{"font_size": 100}, "font_size", "must be <= 48"},
		{protocol.FrontSSH, map[string]any{"keepalive": "2h"}, "keepalive", "must be <= 1h"},
		{protocol.FrontSSH, map[string]any{"host_key": "not a key"}, "host_key", "a public key"},
		{protocol.FrontSSH, map[string]any{"host_key": "ssh-ed25519 !!!"}, "host_key", "no base64"},
		{protocol.FrontSSH, map[string]any{"command": strings.Repeat("x", maxCommand+1)}, "command", "longer than 2048"},
		{protocol.FrontRDP, map[string]any{"workstation_name": "WS 01"}, "workstation_name", "letters, digits and hyphens"},
		{protocol.FrontRDP, map[string]any{"workstation_name": `CORP\WS01`}, "workstation_name", "letters, digits and hyphens"},
		{protocol.FrontRDP, map[string]any{"workstation_name": "123456"}, "workstation_name", "not digits alone"},
		{protocol.FrontRDP, map[string]any{"workstation_name": "DESKTOP-4F7Q2KDX"}, "workstation_name", "longer than 15"},
		{protocol.FrontRDP, map[string]any{"timezone": "Mars/Olympus_Mons"}, "timezone", "no such time zone"},
		{protocol.FrontRDP, map[string]any{"timezone": "Local"}, "timezone", "as IANA names it"},
		{protocol.FrontVNC, map[string]any{"timezone": "UTC"}, "timezone", "not a declared field"},
		{protocol.FrontRDP, map[string]any{"client_name": "logos.lan"}, "client_name", "letters, digits and hyphens"},
		{protocol.FrontRDP, map[string]any{"client_name": "DESKTOP-4F7Q2KDX"}, "client_name", "longer than 15"},
		{protocol.FrontTelnet, map[string]any{"login_failure": "(["}, "login_failure", "not a regular expression"},
		{protocol.FrontForward, map[string]any{"quality": "best"}, "quality", "not a declared field"},
	} {
		got, problems := Params(c.front, c.values, "core", 41000)
		if got != nil || !strings.Contains(problems[c.key], c.reason) {
			t.Errorf("%s %s: %v %v, want %s", c.front, c.key, got, problems, c.reason)
		}
	}
}

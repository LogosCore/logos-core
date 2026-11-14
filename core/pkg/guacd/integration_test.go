package guacd

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestIntegrationGuacd opens a session in a real guacd
// (INTEGRATION_GUACD, host:port): an SSH login (or INTEGRATION_GUACD_PROTOCOL's)
// to INTEGRATION_GUACD_HOST (port 80 of a web server will do), which guacd
// takes, says ready to, then ends with an error instruction when the server
// speaks no SSH. It pins the handshake to what guacd's version expects.
func TestIntegrationGuacd(t *testing.T) {
	addr, host := os.Getenv("INTEGRATION_GUACD"), os.Getenv("INTEGRATION_GUACD_HOST")
	if addr == "" || host == "" {
		t.Skip("INTEGRATION_GUACD and INTEGRATION_GUACD_HOST not set; skipping the guacd integration test")
	}
	// ssh by default; INTEGRATION_GUACD_PROTOCOL another (rdp: its plugin,
	// FreeRDP's, loads and fails to connect).
	protocol := os.Getenv("INTEGRATION_GUACD_PROTOCOL")
	if protocol == "" {
		protocol = "ssh"
	}
	// INTEGRATION_GUACD_PORT: the host's port (80 by default; a closed one
	// fails RDP at once, where port 80 keeps it waiting).
	port := os.Getenv("INTEGRATION_GUACD_PORT")
	if port == "" {
		port = "80"
	}
	s, err := Open(context.Background(), addr, Config{Protocol: protocol, Width: 800, Height: 600, DPI: 96, Timezone: "Europe/Kyiv", Name: "itest",
		// RDP takes the server's certificate, as core's desktops do.
		Params: map[string]string{"hostname": host, "port": port, "username": "nobody", "password": "x", "ignore-cert": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if !strings.HasPrefix(s.Version, "VERSION_1_") || !strings.HasPrefix(s.ID, "$") {
		t.Fatalf("session %+v", s)
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	r := bufio.NewReader(s)
	for {
		ins, err := Read(r)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		t.Logf("guacd: %s", ins)
		if ins.Opcode == "error" {
			return
		}
	}
}

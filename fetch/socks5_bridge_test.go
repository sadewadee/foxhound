//go:build playwright

package fetch

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	xnetproxy "golang.org/x/net/proxy"
)

func TestNeedsSocks5Bridge(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantOk   bool
		wantUp   string
		wantUser string
		wantPass string
	}{
		{
			name:     "socks5 with auth",
			url:      "socks5://bob:secret@10.0.0.1:1080",
			wantOk:   true,
			wantUp:   "10.0.0.1:1080",
			wantUser: "bob",
			wantPass: "secret",
		},
		{
			name:     "socks5h with auth",
			url:      "socks5h://alice:pw123@proxy.example.com:9050",
			wantOk:   true,
			wantUp:   "proxy.example.com:9050",
			wantUser: "alice",
			wantPass: "pw123",
		},
		{
			name:   "socks5 without auth",
			url:    "socks5://10.0.0.1:1080",
			wantOk: false,
		},
		{
			name:   "socks5 username only",
			url:    "socks5://bob@10.0.0.1:1080",
			wantOk: false,
		},
		{
			name:   "http with auth",
			url:    "http://user:pass@proxy.com:8080",
			wantOk: false,
		},
		{
			name:   "https with auth",
			url:    "https://user:pass@proxy.com:8080",
			wantOk: false,
		},
		{
			name:   "empty string",
			url:    "",
			wantOk: false,
		},
		{
			name:   "malformed url",
			url:    "://broken",
			wantOk: false,
		},
		{
			name:   "socks5 empty password",
			url:    "socks5://bob:@10.0.0.1:1080",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream, user, pass, ok := needsSocks5Bridge(tt.url)
			if ok != tt.wantOk {
				t.Fatalf("needsSocks5Bridge(%q) ok = %v, want %v", tt.url, ok, tt.wantOk)
			}
			if !tt.wantOk {
				return
			}
			if upstream != tt.wantUp {
				t.Errorf("upstream = %q, want %q", upstream, tt.wantUp)
			}
			if user != tt.wantUser {
				t.Errorf("user = %q, want %q", user, tt.wantUser)
			}
			if pass != tt.wantPass {
				t.Errorf("pass = %q, want %q", pass, tt.wantPass)
			}
		})
	}
}

func TestSocks5BridgeStartClose(t *testing.T) {
	// Use a dummy upstream address — we won't actually dial it.
	bridge, err := newSocks5Bridge("127.0.0.1:19999", "user", "pass")
	if err != nil {
		t.Fatalf("newSocks5Bridge: %v", err)
	}

	// Verify the bridge is listening.
	conn, err := net.Dial("tcp", bridge.localAddr)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	conn.Close()

	// Close and verify listener is shut down.
	if err := bridge.Close(); err != nil {
		t.Fatalf("bridge.Close: %v", err)
	}

	_, err = net.Dial("tcp", bridge.localAddr)
	if err == nil {
		t.Fatal("expected dial to fail after Close, but it succeeded")
	}
}

// TestSocks5BridgeRelay tests end-to-end: HTTP server ← upstream SOCKS5 ← bridge ← client.
func TestSocks5BridgeRelay(t *testing.T) {
	// 1. Start a test HTTP server.
	const want = "hello from target"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, want)
	}))
	defer ts.Close()

	// 2. Start a minimal mock SOCKS5 server (no auth) that the bridge will connect to.
	// This simulates the "upstream authenticated SOCKS5 proxy".
	mockUpstream := startMockSocks5(t)
	defer mockUpstream.Close()

	// 3. Start the bridge pointing at the mock upstream.
	bridge, err := newSocks5Bridge(mockUpstream.Addr().String(), "testuser", "testpass")
	if err != nil {
		t.Fatalf("newSocks5Bridge: %v", err)
	}
	defer bridge.Close()

	// 4. Connect through the bridge using x/net/proxy as a SOCKS5 client (no auth).
	dialer, err := xnetproxy.SOCKS5("tcp", bridge.localAddr, nil, xnetproxy.Direct)
	if err != nil {
		t.Fatalf("SOCKS5 dialer: %v", err)
	}

	// Parse target host:port from the test HTTP server.
	conn, err := dialer.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial through bridge: %v", err)
	}
	defer conn.Close()

	// Send a raw HTTP request.
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", ts.Listener.Addr().String())
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}

	body, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	got := string(body)
	if len(got) == 0 {
		t.Fatal("empty response")
	}
	if !contains(got, want) {
		t.Errorf("response does not contain %q:\n%s", want, got)
	}
}

func TestSocks5BridgeUnsupportedCmd(t *testing.T) {
	bridge, err := newSocks5Bridge("127.0.0.1:19999", "user", "pass")
	if err != nil {
		t.Fatalf("newSocks5Bridge: %v", err)
	}
	defer bridge.Close()

	conn, err := net.Dial("tcp", bridge.localAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send greeting.
	conn.Write([]byte{0x05, 0x01, 0x00})

	// Read greeting reply.
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		t.Fatalf("read greeting reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("unexpected greeting reply: %x", reply)
	}

	// Send BIND command (0x02) instead of CONNECT (0x01).
	conn.Write([]byte{
		0x05, 0x02, 0x00, 0x01, // VER=5, CMD=BIND, RSV=0, ATYP=IPv4
		127, 0, 0, 1, // dst addr
		0x00, 0x50, // dst port 80
	})

	// Read reply — expect REP=0x07 (command not supported).
	var cmdReply [10]byte
	if _, err := io.ReadFull(conn, cmdReply[:]); err != nil {
		t.Fatalf("read cmd reply: %v", err)
	}
	if cmdReply[1] != 0x07 {
		t.Errorf("expected REP=0x07 (command not supported), got 0x%02x", cmdReply[1])
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsBytes([]byte(s), []byte(substr)))
}

func containsBytes(s, sub []byte) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if string(s[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}

// startMockSocks5 starts a minimal SOCKS5 server that accepts username/password
// auth and proxies CONNECT requests to the real target. This simulates what
// a real upstream authenticated SOCKS5 proxy would do.
func startMockSocks5(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock socks5 listen: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleMockSocks5(conn)
		}
	}()

	return ln
}

func handleMockSocks5(conn net.Conn) {
	defer conn.Close()

	// Greeting: read VER + NMETHODS + METHODS.
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	methods := make([]byte, hdr[1])
	io.ReadFull(conn, methods)

	// Check if client offers username/password auth (0x02).
	hasUserPass := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserPass = true
			break
		}
	}

	if hasUserPass {
		// Select username/password auth.
		conn.Write([]byte{0x05, 0x02})

		// Username/password sub-negotiation (RFC 1929).
		var ver [1]byte
		io.ReadFull(conn, ver[:])
		var ulen [1]byte
		io.ReadFull(conn, ulen[:])
		uname := make([]byte, ulen[0])
		io.ReadFull(conn, uname)
		var plen [1]byte
		io.ReadFull(conn, plen[:])
		passwd := make([]byte, plen[0])
		io.ReadFull(conn, passwd)
		// Always accept.
		conn.Write([]byte{0x01, 0x00})
	} else {
		// No auth.
		conn.Write([]byte{0x05, 0x00})
	}

	// Request: VER CMD RSV ATYP DST.ADDR DST.PORT
	var req [4]byte
	if _, err := io.ReadFull(conn, req[:]); err != nil {
		return
	}
	if req[1] != 0x01 { // only CONNECT
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var target string
	switch req[3] {
	case 0x01:
		var ip [4]byte
		io.ReadFull(conn, ip[:])
		target = net.IP(ip[:]).String()
	case 0x03:
		var l [1]byte
		io.ReadFull(conn, l[:])
		d := make([]byte, l[0])
		io.ReadFull(conn, d)
		target = string(d)
	case 0x04:
		var ip [16]byte
		io.ReadFull(conn, ip[:])
		target = net.IP(ip[:]).String()
	}

	var portBytes [2]byte
	io.ReadFull(conn, portBytes[:])
	port := binary.BigEndian.Uint16(portBytes[:])
	target = fmt.Sprintf("%s:%d", target, port)

	// Dial the real target.
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	// Success.
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Relay — close both sides when either direction finishes.
	done := make(chan struct{})
	go func() {
		io.Copy(upstream, conn)
		upstream.Close()
		close(done)
	}()
	io.Copy(conn, upstream)
	conn.Close()
	<-done
}

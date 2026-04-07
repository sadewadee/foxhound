//go:build playwright

// socks5_bridge.go — transparent local SOCKS5 proxy bridge for authenticated
// upstream proxies.
//
// Firefox (and therefore Camoufox via Playwright) does not support SOCKS5
// proxies with username/password authentication. When the user configures
// socks5://user:pass@host:port, this bridge automatically starts a local
// unauthenticated SOCKS5 listener and relays traffic to the upstream proxy
// with credentials. The browser connects to the local bridge (no auth)
// while the bridge handles upstream authentication transparently.
//
// Detection is automatic: needsSocks5Bridge parses the proxy URL and returns
// true when the scheme is socks5/socks5h and credentials are present.
// No flags or config options are required.

package fetch

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	neturl "net/url"
	"sync"

	xnetproxy "golang.org/x/net/proxy"
)

// socks5Bridge is a local unauthenticated SOCKS5 proxy that forwards
// connections to an upstream SOCKS5 proxy with authentication.
type socks5Bridge struct {
	listener     net.Listener
	upstreamAddr string           // "host:port"
	auth         *xnetproxy.Auth  // upstream credentials
	dialer       xnetproxy.Dialer // cached upstream dialer (created once)
	cancel       context.CancelFunc
	wg           sync.WaitGroup // tracks in-flight relay goroutines AND serve goroutine
	localAddr    string         // "127.0.0.1:<port>"
}

// needsSocks5Bridge inspects a proxy URL and returns the upstream address and
// credentials if a local bridge is needed. Returns ok=true only when the scheme
// is socks5 or socks5h and both username and password are present.
func needsSocks5Bridge(rawURL string) (upstream, user, pass string, ok bool) {
	if rawURL == "" {
		return "", "", "", false
	}
	u, err := neturl.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", "", "", false
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return "", "", "", false
	}
	if u.User == nil {
		return "", "", "", false
	}
	user = u.User.Username()
	pass, hasPass := u.User.Password()
	if user == "" || !hasPass || pass == "" {
		return "", "", "", false
	}
	return u.Host, user, pass, true
}

// newSocks5Bridge starts a local SOCKS5 listener on 127.0.0.1 with an
// OS-assigned port. It begins accepting connections immediately in a
// background goroutine. The caller must call Close when done.
func newSocks5Bridge(upstreamAddr, username, password string) (*socks5Bridge, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("socks5 bridge: listen: %w", err)
	}

	auth := &xnetproxy.Auth{User: username, Password: password}
	dialer, dialerErr := xnetproxy.SOCKS5("tcp", upstreamAddr, auth, xnetproxy.Direct)
	if dialerErr != nil {
		ln.Close()
		return nil, fmt.Errorf("socks5 bridge: create dialer: %w", dialerErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	b := &socks5Bridge{
		listener:     ln,
		upstreamAddr: upstreamAddr,
		auth:         auth,
		dialer:       dialer,
		cancel:       cancel,
		localAddr:    ln.Addr().String(),
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.serve(ctx)
	}()
	return b, nil
}

// serve accepts connections until the context is cancelled or the listener is
// closed. Each connection is handled in its own goroutine.
func (b *socks5Bridge) serve(ctx context.Context) {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			// Expected when Close() shuts down the listener.
			if ctx.Err() != nil {
				return
			}
			// Transient accept error — keep going.
			slog.Debug("socks5 bridge: accept error", "err", err)
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.handleConn(conn)
		}()
	}
}

// handleConn implements server-side SOCKS5 (RFC 1928) for the CONNECT command
// only. It negotiates no-auth with the client, reads the target address, dials
// the upstream authenticated SOCKS5 proxy, and relays bytes bidirectionally.
func (b *socks5Bridge) handleConn(conn net.Conn) {
	// conn is closed explicitly in the relay phase (or on early return).
	var connClosed bool
	closeConn := func() {
		if !connClosed {
			conn.Close()
			connClosed = true
		}
	}
	defer closeConn()

	// --- Step 1: Client greeting ---
	// +----+----------+----------+
	// |VER | NMETHODS | METHODS  |
	// +----+----------+----------+
	// | 1  |    1     | 1..255   |
	// +----+----------+----------+
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return
	}
	if header[0] != 0x05 { // not SOCKS5
		return
	}
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}

	// Reply: no authentication required (0x00).
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// --- Step 2: Client request ---
	// +----+-----+-------+------+----------+----------+
	// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+
	var req [4]byte
	if _, err := io.ReadFull(conn, req[:]); err != nil {
		return
	}
	if req[0] != 0x05 {
		return
	}
	if req[1] != 0x01 { // only CONNECT supported
		b.socksReply(conn, 0x07) // command not supported
		return
	}

	// Parse target address based on ATYP.
	var targetAddr string
	switch req[3] {
	case 0x01: // IPv4
		var ip [4]byte
		if _, err := io.ReadFull(conn, ip[:]); err != nil {
			return
		}
		targetAddr = net.IP(ip[:]).String()
	case 0x03: // Domain name
		var lenByte [1]byte
		if _, err := io.ReadFull(conn, lenByte[:]); err != nil {
			return
		}
		domain := make([]byte, lenByte[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return
		}
		targetAddr = string(domain)
	case 0x04: // IPv6
		var ip [16]byte
		if _, err := io.ReadFull(conn, ip[:]); err != nil {
			return
		}
		targetAddr = net.IP(ip[:]).String()
	default:
		b.socksReply(conn, 0x08) // address type not supported
		return
	}

	// Read port (2 bytes, big-endian).
	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes[:])
	target := fmt.Sprintf("%s:%d", targetAddr, port)

	// --- Step 3: Dial upstream via authenticated SOCKS5 (cached dialer) ---
	upstream, err := b.dialer.Dial("tcp", target)
	if err != nil {
		slog.Debug("socks5 bridge: upstream dial failed", "target", target, "err", err)
		b.socksReply(conn, 0x05) // connection refused
		return
	}

	// --- Step 4: Success reply ---
	b.socksReply(conn, 0x00)

	// --- Step 5: Bidirectional relay ---
	// When either direction hits EOF or error, close both sides so the
	// other io.Copy unblocks promptly. Each conn is closed exactly once.
	done := make(chan struct{})
	go func() {
		io.Copy(upstream, conn) //nolint:errcheck
		upstream.Close()
		close(done)
	}()
	io.Copy(conn, upstream) //nolint:errcheck
	closeConn()
	<-done
}

// socksReply sends a SOCKS5 reply with the given status code.
// Bound address is always 0.0.0.0:0 since browsers don't inspect it.
func (b *socks5Bridge) socksReply(conn net.Conn, rep byte) {
	// +----+-----+-------+------+----------+----------+
	// |VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
	// +----+-----+-------+------+----------+----------+
	reply := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(reply) //nolint:errcheck
}

// Close stops the bridge listener and waits for in-flight connections to drain.
func (b *socks5Bridge) Close() error {
	// Cancel first so serve() sees ctx.Err() != nil when Accept unblocks.
	b.cancel()
	err := b.listener.Close()
	// Wait for the serve goroutine and all in-flight connection handlers.
	b.wg.Wait()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("socks5 bridge: close listener: %w", err)
	}
	return nil
}

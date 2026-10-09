package api

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync"
	"time"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// SetDebugWriter enables verbose request tracing (DNS, connect, TLS, first
// byte, status, headers) written to w. Pass nil to disable.
func (c *Client) SetDebugWriter(w io.Writer) {
	c.debug = w
}

// tracer writes timestamped request-lifecycle events. A nil *tracer is a
// valid no-op so call sites don't need to guard on whether debug is enabled.
type tracer struct {
	w     io.Writer
	start time.Time
	mu    sync.Mutex
}

func newTracer(w io.Writer) *tracer {
	if w == nil {
		return nil
	}
	return &tracer{w: w, start: time.Now()}
}

// logf writes one line: wall-clock UTC time (to correlate with server-side
// created_at/processed_at), elapsed since the request started, and the
// message with any api_key value redacted.
func (t *tracer) logf(format string, args ...any) {
	if t == nil {
		return
	}
	now := time.Now()
	msg := clierrors.RedactAPIKey(fmt.Sprintf(format, args...))
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.w, "[debug] %s +%6.3fs %s\n",
		now.UTC().Format("2006-01-02T15:04:05.000Z"), now.Sub(t.start).Seconds(), msg)
}

// clientTrace returns httptrace hooks that report each connection phase.
// DNS/connect hooks are not fired for connections made via an HTTP proxy;
// GotConn still is, which is why the proxy is logged separately in doGet.
func (t *tracer) clientTrace() *httptrace.ClientTrace {
	if t == nil {
		return nil
	}
	return &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			t.logf("DNS lookup: %s", info.Host)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if info.Err != nil {
				t.logf("DNS failed: %v", info.Err)
				return
			}
			addrs := make([]string, 0, len(info.Addrs))
			for _, a := range info.Addrs {
				addrs = append(addrs, a.String())
			}
			t.logf("DNS resolved: %s", strings.Join(addrs, ", "))
		},
		ConnectStart: func(network, addr string) {
			t.logf("Connecting: %s %s", network, addr)
		},
		ConnectDone: func(network, addr string, err error) {
			if err != nil {
				t.logf("Connect failed: %s %s: %v", network, addr, err)
				return
			}
			t.logf("Connected: %s %s", network, addr)
		},
		TLSHandshakeStart: func() {
			t.logf("TLS handshake started")
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			if err != nil {
				t.logf("TLS handshake failed: %v", err)
				return
			}
			t.logf("TLS handshake done: %s, ALPN=%q", tls.VersionName(state.Version), state.NegotiatedProtocol)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			remote := "unknown"
			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				remote = info.Conn.RemoteAddr().String()
			}
			if info.Reused {
				t.logf("Using connection: %s (reused, idle %s)", remote, info.IdleTime.Round(time.Millisecond))
				return
			}
			t.logf("Using connection: %s (new)", remote)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err != nil {
				t.logf("Write request failed: %v", info.Err)
				return
			}
			t.logf("Request sent, waiting for response headers")
		},
		GotFirstResponseByte: func() {
			t.logf("First response byte received")
		},
	}
}

// logResponse reports the status line and headers in a stable order.
func (t *tracer) logResponse(resp *http.Response) {
	if t == nil || resp == nil {
		return
	}
	t.logf("Response: %s %s", resp.Proto, resp.Status)
	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range resp.Header[name] {
			t.logf("  %s: %s", name, value)
		}
	}
}

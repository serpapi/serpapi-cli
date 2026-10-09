package api

import (
	"crypto/tls"
	"encoding/json"
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

	// Captured so the server-side search_metadata timestamps can be
	// correlated with the client-side timeline afterwards.
	sentAt      time.Time
	firstByteAt time.Time
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
			local, remote := "unknown", "unknown"
			if info.Conn != nil {
				if a := info.Conn.LocalAddr(); a != nil {
					local = a.String()
				}
				if a := info.Conn.RemoteAddr(); a != nil {
					remote = a.String()
				}
			}
			if info.Reused {
				t.logf("Using connection: %s -> %s (reused, idle %s)", local, remote, info.IdleTime.Round(time.Millisecond))
				return
			}
			t.logf("Using connection: %s -> %s (new)", local, remote)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err != nil {
				t.logf("Write request failed: %v", info.Err)
				return
			}
			t.mu.Lock()
			t.sentAt = time.Now()
			t.mu.Unlock()
			t.logf("Request sent, waiting for response headers")
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			t.firstByteAt = time.Now()
			t.mu.Unlock()
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

// searchMetadataTimeLayout matches search_metadata.created_at / processed_at,
// e.g. "2026-10-09 11:01:54 UTC". Second resolution only.
const searchMetadataTimeLayout = "2006-01-02 15:04:05 MST"

// logSearchMetadata correlates the server-side search_metadata timestamps
// with the client-side timeline so a stall can be attributed to one of:
//   - before the server created the search (network, edge, queueing)
//   - server processing (created_at -> processed_at)
//   - after processing finished but before headers were sent
//
// Server timestamps have 1s resolution, so sub-second deltas are noise.
func (t *tracer) logSearchMetadata(body []byte) {
	if t == nil {
		return
	}
	var envelope struct {
		Metadata struct {
			ID             string  `json:"id"`
			Status         string  `json:"status"`
			CreatedAt      string  `json:"created_at"`
			ProcessedAt    string  `json:"processed_at"`
			TotalTimeTaken float64 `json:"total_time_taken"`
		} `json:"search_metadata"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Metadata.ID == "" {
		return
	}
	m := envelope.Metadata

	t.mu.Lock()
	sentAt, firstByteAt := t.sentAt, t.firstByteAt
	t.mu.Unlock()

	t.logf("Server search: id=%s status=%s total_time_taken=%gs", m.ID, m.Status, m.TotalTimeTaken)

	createdAt, err1 := time.Parse(searchMetadataTimeLayout, m.CreatedAt)
	processedAt, err2 := time.Parse(searchMetadataTimeLayout, m.ProcessedAt)
	if err1 != nil || err2 != nil || sentAt.IsZero() || firstByteAt.IsZero() {
		t.logf("Server timeline: created_at=%q processed_at=%q (could not correlate)", m.CreatedAt, m.ProcessedAt)
		return
	}

	t.logf("Server timeline: created_at=%s (%s after request sent), processed_at=%s (%s processing), headers received %s after processed_at",
		createdAt.Format("15:04:05Z"), signedSeconds(createdAt.Sub(sentAt)),
		processedAt.Format("15:04:05Z"), signedSeconds(processedAt.Sub(createdAt)),
		signedSeconds(firstByteAt.Sub(processedAt)))

	// Flag the phase that dominated when the wait was noticeably longer than
	// the server's own processing time. 2s absorbs the 1s timestamp
	// resolution plus typical network latency.
	const slack = 2 * time.Second
	waited := firstByteAt.Sub(sentAt)
	if waited <= time.Duration(m.TotalTimeTaken*float64(time.Second))+slack {
		return
	}
	switch {
	case createdAt.Sub(sentAt) > slack:
		t.logf("Note: most of the wait (%s) happened before the server created the search; suspect network path, proxy, or server-side queueing rather than the search engine", signedSeconds(createdAt.Sub(sentAt)))
	case firstByteAt.Sub(processedAt) > slack:
		t.logf("Note: the server finished processing %s before headers arrived; suspect response delivery path", signedSeconds(firstByteAt.Sub(processedAt)))
	}
}

// signedSeconds formats d as e.g. "+1.2s" or "-0.4s". The sign is kept
// because clock skew between client and server can make deltas negative.
func signedSeconds(d time.Duration) string {
	return fmt.Sprintf("%+.1fs", d.Seconds())
}

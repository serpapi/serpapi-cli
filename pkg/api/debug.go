package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// WithDebug traces each request to w: DNS, connect, TLS, first byte, status
// and headers, and the search_metadata timeline. With jsonLines set, every
// line is one JSON object, {"debug":{"at":...,"elapsed":...,"event":...}},
// so stderr stays parseable line by line off a terminal; otherwise lines are
// plain text, "[debug] <at> +<elapsed>s <event>". Passing a nil w disables it.
func WithDebug(w io.Writer, jsonLines bool) Option {
	return func(c *Client) {
		c.debug = w
		c.debugJSON = jsonLines
	}
}

// debugEvent is one JSON debug line. Headers and the search timeline carry
// structured fields next to the human-readable event.
type debugEvent struct {
	At      string         `json:"at"`
	Elapsed float64        `json:"elapsed"`
	Event   string         `json:"event"`
	Data    map[string]any `json:"data,omitempty"`
}

// tracer writes timestamped request-lifecycle events. A nil *tracer is a
// valid no-op so call sites don't need to guard on whether debug is enabled.
type tracer struct {
	w     io.Writer
	json  bool
	key   string
	start time.Time
	mu    sync.Mutex

	// Captured so the server-side search_metadata timestamps can be
	// correlated with the client-side timeline afterwards.
	sentAt      time.Time
	firstByteAt time.Time
}

func (c *Client) newTracer() *tracer {
	if c.debug == nil {
		return nil
	}
	return &tracer{w: c.debug, json: c.debugJSON, key: c.apiKey, start: time.Now()}
}

// logf writes one event with no structured data.
func (t *tracer) logf(format string, args ...any) {
	t.log(nil, format, args...)
}

// log writes one event: wall-clock UTC time (to correlate with server-side
// created_at/processed_at), seconds since the request started, and the
// message, with the API key redacted from it and from data. data, if not nil,
// is included in the JSON form only.
func (t *tracer) log(data map[string]any, format string, args ...any) {
	if t == nil {
		return
	}
	now := time.Now()
	at := now.UTC().Format("2006-01-02T15:04:05.000Z")
	elapsed := now.Sub(t.start).Seconds()
	msg := t.redact(fmt.Sprintf(format, args...))
	for k, v := range data {
		switch v := v.(type) {
		case string:
			data[k] = t.redact(v)
		case map[string]string:
			redacted := make(map[string]string, len(v))
			for name, value := range v {
				redacted[t.redact(name)] = t.redact(value)
			}
			data[k] = redacted
		}
	}

	var line []byte
	if t.json {
		b, err := json.Marshal(map[string]any{"debug": debugEvent{At: at, Elapsed: round3(elapsed), Event: msg, Data: data}})
		if err != nil {
			return
		}
		line = append(b, '\n')
	} else {
		line = fmt.Appendf(nil, "[debug] %s +%6.3fs %s\n", at, elapsed, msg)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.w.Write(line)
}

// redact replaces api_key values in s, and the key itself, plain or
// URL-encoded: a header such as a redirect's Location can carry it anywhere.
func (t *tracer) redact(s string) string {
	s = clierrors.RedactAPIKey(s)
	if t.key == "" {
		return s
	}
	return strings.NewReplacer(t.key, "[REDACTED]", url.QueryEscape(t.key), "[REDACTED]").Replace(s)
}

func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}

// clientTrace returns httptrace hooks that report each connection phase.
// DNS/connect hooks are not fired for connections made via an HTTP proxy;
// GotConn still is, which is why the proxy is logged separately in get.
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
			data := map[string]any{"local": local, "remote": remote, "reused": info.Reused}
			if info.Reused {
				t.log(data, "Using connection: %s -> %s (reused, idle %s)", local, remote, info.IdleTime.Round(time.Millisecond))
				return
			}
			t.log(data, "Using connection: %s -> %s (new)", local, remote)
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

// logRequest reports what is about to be sent.
func (t *tracer) logRequest(req *http.Request, timeout time.Duration) {
	if t == nil {
		return
	}
	t.logf("GET %s", req.URL.String())
	t.logf("User-Agent: %s", req.UserAgent())
	if timeout > 0 {
		t.logf("Timeout: %gs", timeout.Seconds())
	} else {
		t.logf("Timeout: disabled")
	}
	if proxyURL, err := http.ProxyFromEnvironment(req); err == nil && proxyURL != nil {
		t.logf("Proxy: %s", proxyURL.Redacted())
	}
}

// logResponse reports the status line and headers in a stable order. The
// JSON form carries them as one event; the text form lists one per line.
func (t *tracer) logResponse(resp *http.Response) {
	if t == nil || resp == nil {
		return
	}
	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		names = append(names, name)
	}
	sort.Strings(names)

	if t.json {
		headers := make(map[string]string, len(names))
		for _, name := range names {
			headers[name] = strings.Join(resp.Header[name], ", ")
		}
		t.log(map[string]any{"status": resp.StatusCode, "proto": resp.Proto, "headers": headers},
			"Response: %s %s", resp.Proto, resp.Status)
		return
	}
	t.logf("Response: %s %s", resp.Proto, resp.Status)
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
// with the client-side timeline so a stall can be attributed, if this
// machine's clock agrees with SerpApi's, to one of:
//   - before the request reached the server (DNS, VPN, proxy, network path)
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

	t.log(map[string]any{"id": m.ID, "status": m.Status, "total_time_taken": m.TotalTimeTaken},
		"Server search: id=%s status=%s total_time_taken=%gs", m.ID, m.Status, m.TotalTimeTaken)

	createdAt, err1 := time.Parse(searchMetadataTimeLayout, m.CreatedAt)
	processedAt, err2 := time.Parse(searchMetadataTimeLayout, m.ProcessedAt)
	if err1 != nil || err2 != nil || sentAt.IsZero() || firstByteAt.IsZero() {
		t.logf("Server timeline: created_at=%q processed_at=%q (could not correlate)", m.CreatedAt, m.ProcessedAt)
		return
	}

	beforeCreate := createdAt.Sub(sentAt)
	processing := processedAt.Sub(createdAt)
	afterProcess := firstByteAt.Sub(processedAt)

	// A search created well before this request was sent is a cached result,
	// whose timestamps describe the original search, or this machine's clock
	// is that far ahead of SerpApi's; the timestamps can't tell which.
	if beforeCreate < -5*time.Second {
		t.log(map[string]any{
			"created_at":        createdAt.Format(time.RFC3339),
			"processed_at":      processedAt.Format(time.RFC3339),
			"sent_to_created_s": round3(beforeCreate.Seconds()),
		}, "Server timeline: created_at=%s, %.0fs before this request was sent: a cached result, or this machine's clock is ahead of SerpApi's; headers arrived %s after the request was sent",
			createdAt.Format(time.RFC3339), (-beforeCreate).Seconds(), signedSeconds(firstByteAt.Sub(sentAt)))
		return
	}
	t.log(map[string]any{
		"created_at":             createdAt.Format(time.RFC3339),
		"processed_at":           processedAt.Format(time.RFC3339),
		"sent_to_created_s":      round3(beforeCreate.Seconds()),
		"processing_s":           round3(processing.Seconds()),
		"processed_to_headers_s": round3(afterProcess.Seconds()),
	}, "Server timeline: created_at=%s (%s after request sent), processed_at=%s (%s processing), headers received %s after processed_at",
		createdAt.Format("15:04:05Z"), signedSeconds(beforeCreate),
		processedAt.Format("15:04:05Z"), signedSeconds(processing),
		signedSeconds(afterProcess))

	// Flag the phase that dominated when the wait was noticeably longer than
	// the server's own processing time. 2s absorbs the 1s timestamp
	// resolution plus typical network latency. The wait and the processing
	// time each come from one clock; which phase the rest fell in compares
	// the two, so a skewed clock moves it.
	const slack = 2 * time.Second
	waited := firstByteAt.Sub(sentAt)
	if waited <= time.Duration(m.TotalTimeTaken*float64(time.Second))+slack {
		return
	}
	switch {
	case beforeCreate < -slack || afterProcess < -slack:
		t.logf("Note: waited %s for a search SerpApi processed in %gs, but this machine's clock and SerpApi's disagree, so the timeline can't say where the rest went", signedSeconds(waited), m.TotalTimeTaken)
	case beforeCreate > slack:
		t.logf("Note: if this machine's clock matches SerpApi's, most of the wait (%s) happened before the request reached SerpApi; check the network path (DNS, VPN, proxy, connectivity)", signedSeconds(beforeCreate))
	case afterProcess > slack:
		t.logf("Note: if this machine's clock matches SerpApi's, the response took %s to arrive after SerpApi finished processing; check the network path back to this machine", signedSeconds(afterProcess))
	}
}

// signedSeconds formats d as e.g. "+1.2s" or "-0.4s". The sign is kept
// because clock skew between client and server can make deltas negative.
func signedSeconds(d time.Duration) string {
	return fmt.Sprintf("%+.1fs", d.Seconds())
}

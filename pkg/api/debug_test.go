package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func newDebugServer(t *testing.T, w *bytes.Buffer, jsonLines bool) *Client {
	t.Helper()
	srv := newTestServer(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.Header().Set("Serpapi-Search-Id", "abc123")
		_, _ = rw.Write([]byte(`{"ok":true}`))
	})
	return New("secret_key", WithBaseURL(srv.URL), WithTimeout(5*time.Second), WithDebug(w, jsonLines))
}

func TestDebugDisabledByDefault(t *testing.T) {
	var buf bytes.Buffer
	client := newDebugServer(t, &buf, false)
	WithDebug(nil, false)(client)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no debug output, got:\n%s", buf.String())
	}
}

func TestDebugTraceCoversRequestLifecycle(t *testing.T) {
	var buf bytes.Buffer
	client := newDebugServer(t, &buf, false)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"GET http",
		"Timeout: 5s",
		"Connecting:",
		"Connected:",
		"Using connection:",
		"Request sent, waiting for response headers",
		"First response byte received",
		"Response: HTTP/1.1 200 OK",
		"Serpapi-Search-Id: abc123",
		"Body received: 11 bytes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret_key") || !strings.Contains(out, "api_key=[REDACTED]") {
		t.Errorf("debug output must redact the API key:\n%s", out)
	}
	// Local and remote addresses are both reported, e.g. "127.0.0.1:54321 -> 127.0.0.1:8080 (new)".
	if !regexp.MustCompile(`Using connection: 127\.0\.0\.1:\d+ -> 127\.0\.0\.1:\d+ \(new\)`).MatchString(out) {
		t.Errorf("expected local -> remote address pair:\n%s", out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "[debug] ") {
			t.Errorf("every debug line should be prefixed, got %q", line)
		}
	}
}

// debugLine is the JSON form of one trace line.
type debugLine struct {
	Debug struct {
		At      string         `json:"at"`
		Elapsed float64        `json:"elapsed"`
		Event   string         `json:"event"`
		Data    map[string]any `json:"data"`
	} `json:"debug"`
}

// parseDebugLines requires every line of out to be a {"debug":{...}} object.
func parseDebugLines(t *testing.T, out string) []debugLine {
	t.Helper()
	var lines []debugLine
	for _, raw := range strings.Split(strings.TrimSpace(out), "\n") {
		var l debugLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil || l.Debug.Event == "" {
			t.Fatalf("debug line is not a JSON debug object: %q (%v)", raw, err)
		}
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", l.Debug.At); err != nil {
			t.Errorf("bad timestamp %q: %v", l.Debug.At, err)
		}
		lines = append(lines, l)
	}
	return lines
}

func TestDebugJSONLinesStayParseable(t *testing.T) {
	var buf bytes.Buffer
	client := newDebugServer(t, &buf, true)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := parseDebugLines(t, buf.String())

	events := make([]string, 0, len(lines))
	for _, l := range lines {
		events = append(events, l.Debug.Event)
	}
	joined := strings.Join(events, "\n")
	for _, want := range []string{"GET http", "Request sent", "First response byte", "Response: HTTP/1.1 200 OK", "Body received: 11 bytes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("events missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(buf.String(), "secret_key") {
		t.Errorf("JSON debug output must redact the API key:\n%s", buf.String())
	}

	// Headers travel as one structured event rather than one line each.
	var sawHeaders, sawConn bool
	for _, l := range lines {
		if strings.HasPrefix(l.Debug.Event, "Response:") {
			sawHeaders = true
			headers, _ := l.Debug.Data["headers"].(map[string]any)
			if headers["Serpapi-Search-Id"] != "abc123" {
				t.Errorf("expected Serpapi-Search-Id in response data, got %v", l.Debug.Data)
			}
			if status, _ := l.Debug.Data["status"].(float64); status != 200 {
				t.Errorf("expected status 200 in response data, got %v", l.Debug.Data["status"])
			}
		}
		if strings.HasPrefix(l.Debug.Event, "Using connection:") {
			sawConn = true
			if l.Debug.Data["local"] == nil || l.Debug.Data["remote"] == nil {
				t.Errorf("expected local/remote in connection data, got %v", l.Debug.Data)
			}
		}
		if strings.HasPrefix(l.Debug.Event, "  ") {
			t.Errorf("JSON mode should not emit indented header lines: %q", l.Debug.Event)
		}
	}
	if !sawHeaders || !sawConn {
		t.Errorf("expected Response and Using connection events, got:\n%s", joined)
	}
}

func TestDebugTraceReportsTimeout(t *testing.T) {
	var buf bytes.Buffer
	client := newSlowServer(t, 5*time.Second, 50*time.Millisecond)
	WithDebug(&buf, false)(client)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err == nil {
		t.Fatal("expected timeout error")
	}
	out := buf.String()
	if !strings.Contains(out, "Request sent, waiting for response headers") {
		t.Errorf("expected trace to show request was sent before the stall:\n%s", out)
	}
	if !strings.Contains(out, "Request failed:") || !strings.Contains(out, "Timeout exceeded") {
		t.Errorf("expected trace to report the failure:\n%s", out)
	}
	if strings.Contains(out, "First response byte received") {
		t.Errorf("no response byte should have arrived:\n%s", out)
	}
}

// newMetadataServer serves a search response whose created_at/processed_at
// are stamped at request arrival, so the correlation line has real values.
func newMetadataServer(t *testing.T, w *bytes.Buffer, jsonLines bool, processing time.Duration) *Client {
	t.Helper()
	srv := newTestServer(t, func(rw http.ResponseWriter, r *http.Request) {
		created := time.Now().UTC()
		processed := created.Add(processing)
		rw.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(rw, `{"search_metadata":{"id":"abc123","status":"Success","created_at":%q,"processed_at":%q,"total_time_taken":%g}}`,
			created.Format(searchMetadataTimeLayout), processed.Format(searchMetadataTimeLayout), processing.Seconds())
	})
	return New("secret_key", WithBaseURL(srv.URL), WithTimeout(10*time.Second), WithDebug(w, jsonLines))
}

func TestDebugCorrelatesSearchMetadata(t *testing.T) {
	var buf bytes.Buffer
	client := newMetadataServer(t, &buf, false, 1*time.Second)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Server search: id=abc123 status=Success total_time_taken=1s",
		"Server timeline: created_at=",
		"after request sent",
		"(+1.0s processing)",
		"headers received",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Note:") {
		t.Errorf("fast response should not trigger a stall note:\n%s", out)
	}
}

func TestDebugCorrelatesSearchMetadataAsJSON(t *testing.T) {
	var buf bytes.Buffer
	client := newMetadataServer(t, &buf, true, 1*time.Second)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sawSearch, sawTimeline bool
	for _, l := range parseDebugLines(t, buf.String()) {
		switch {
		case strings.HasPrefix(l.Debug.Event, "Server search:"):
			sawSearch = true
			if l.Debug.Data["id"] != "abc123" || l.Debug.Data["total_time_taken"] != 1.0 {
				t.Errorf("unexpected search data: %v", l.Debug.Data)
			}
		case strings.HasPrefix(l.Debug.Event, "Server timeline:"):
			sawTimeline = true
			for _, key := range []string{"created_at", "processed_at", "sent_to_created_s", "processing_s", "processed_to_headers_s"} {
				if _, ok := l.Debug.Data[key]; !ok {
					t.Errorf("timeline data missing %q: %v", key, l.Debug.Data)
				}
			}
			if l.Debug.Data["processing_s"] != 1.0 {
				t.Errorf("expected processing_s=1, got %v", l.Debug.Data["processing_s"])
			}
		}
	}
	if !sawSearch || !sawTimeline {
		t.Errorf("expected Server search and Server timeline events:\n%s", buf.String())
	}
}

// stallTracer builds a tracer with a fixed client-side timeline and a body
// whose server timestamps are offset from it, without any real waiting.
func stallTracer(w *bytes.Buffer, preCreate, processing, postProcess time.Duration) (*tracer, []byte) {
	sentAt := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	created := sentAt.Add(preCreate)
	processed := created.Add(processing)
	firstByte := processed.Add(postProcess)

	tr := &tracer{w: w, start: sentAt, sentAt: sentAt, firstByteAt: firstByte}
	body := fmt.Appendf(nil, `{"search_metadata":{"id":"abc123","status":"Success","created_at":%q,"processed_at":%q,"total_time_taken":%g}}`,
		created.Format(searchMetadataTimeLayout), processed.Format(searchMetadataTimeLayout), processing.Seconds())
	return tr, body
}

func TestDebugFlagsStallBeforeSearchCreation(t *testing.T) {
	var buf bytes.Buffer
	// The customer's scenario: 9s of processing but headers arrive ~30s after
	// sending, with the gap entirely before the server created the search.
	tr, body := stallTracer(&buf, 21*time.Second, 9*time.Second, 0)
	tr.logSearchMetadata(body)

	out := buf.String()
	if !strings.Contains(out, "created_at=12:00:21Z (+21.0s after request sent)") {
		t.Errorf("expected 21s pre-creation gap:\n%s", out)
	}
	if !strings.Contains(out, "(+9.0s processing)") {
		t.Errorf("expected 9s processing:\n%s", out)
	}
	if !strings.Contains(out, "Note: if this machine's clock matches SerpApi's, most of the wait (+21.0s) happened before the request reached SerpApi") {
		t.Errorf("expected pre-creation stall note:\n%s", out)
	}
}

func TestDebugFlagsStallAfterProcessing(t *testing.T) {
	var buf bytes.Buffer
	tr, body := stallTracer(&buf, 0, 2*time.Second, 15*time.Second)
	tr.logSearchMetadata(body)

	out := buf.String()
	if !strings.Contains(out, "headers received +15.0s after processed_at") {
		t.Errorf("expected 15s post-processing gap:\n%s", out)
	}
	if !strings.Contains(out, "Note: if this machine's clock matches SerpApi's, the response took +15.0s to arrive after SerpApi finished processing") {
		t.Errorf("expected post-processing stall note:\n%s", out)
	}
}

func TestDebugNoStallNoteWhenServerProcessingDominates(t *testing.T) {
	var buf bytes.Buffer
	// Long, but legitimately so: processing took 40s and that's all we waited.
	tr, body := stallTracer(&buf, 0, 40*time.Second, 500*time.Millisecond)
	tr.logSearchMetadata(body)

	if strings.Contains(buf.String(), "Note:") {
		t.Errorf("long server processing should not be flagged as a client/network stall:\n%s", buf.String())
	}
}

func TestDebugCachedResultIsNotCorrelated(t *testing.T) {
	var buf bytes.Buffer
	// The server created the search 26 minutes before we asked: a cache hit.
	// Its timestamps describe the original search, so no stall note applies
	// even though "headers after processed_at" is enormous. The round trip
	// is firstByte - sent = (-26m + 1s + 26m + 0.8s) = 1.8s.
	tr, body := stallTracer(&buf, -26*time.Minute, 1*time.Second, 26*time.Minute+800*time.Millisecond)
	tr.logSearchMetadata(body)

	out := buf.String()
	if !strings.Contains(out, "Server timeline: created_at=2026-10-09T11:34:00Z, 1560s before this request was sent: a cached result, or this machine's clock is ahead of SerpApi's") {
		t.Errorf("expected cached-result timeline:\n%s", out)
	}
	if !strings.Contains(out, "headers arrived +1.8s after the request was sent") {
		t.Errorf("expected round-trip wait:\n%s", out)
	}
	if strings.Contains(out, "Note:") || strings.Contains(out, "after processed_at") {
		t.Errorf("cached result must not produce a stall diagnosis:\n%s", out)
	}
}

// The timeline compares this machine's clock with SerpApi's, so a skewed
// clock must not come out as a verdict.
func TestDebugTimelineWithSkewedClocks(t *testing.T) {
	var buf bytes.Buffer
	// A fresh search that took 9s, seen from a clock 6s ahead of SerpApi's.
	tr, body := stallTracer(&buf, -6*time.Second, 9*time.Second, 6500*time.Millisecond)
	tr.logSearchMetadata(body)
	if out := buf.String(); strings.Contains(out, "round trip only") || !strings.Contains(out, "or this machine's clock is ahead of SerpApi's") {
		t.Errorf("a clock 6s ahead must not pass for a cache hit:\n%s", out)
	}

	buf.Reset()
	// A 15s stall after processing, seen from a clock 30s behind SerpApi's.
	tr, body = stallTracer(&buf, 30*time.Second, 2*time.Second, -15*time.Second)
	tr.logSearchMetadata(body)
	if out := buf.String(); strings.Contains(out, "before the request reached SerpApi") ||
		!strings.Contains(out, "Note: waited +17.0s for a search SerpApi processed in 2s, but this machine's clock and SerpApi's disagree") {
		t.Errorf("timestamps that contradict each other must not blame one side:\n%s", out)
	}
}

func TestDebugSearchMetadataUnparseableIsReported(t *testing.T) {
	var buf bytes.Buffer
	srv := newTestServer(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"search_metadata":{"id":"abc123","status":"Success","created_at":"soon","processed_at":"later","total_time_taken":0.5}}`))
	})
	client := New("k", WithBaseURL(srv.URL), WithTimeout(5*time.Second), WithDebug(&buf, false))

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Server search: id=abc123") {
		t.Errorf("id should still be reported:\n%s", out)
	}
	if !strings.Contains(out, "could not correlate") {
		t.Errorf("expected graceful fallback for bad timestamps:\n%s", out)
	}
}

func TestDebugNoMetadataLineForNonSearchEndpoints(t *testing.T) {
	var buf bytes.Buffer
	client := newDebugServer(t, &buf, false)

	if _, err := client.Account(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(buf.String(), "Server search:") {
		t.Errorf("account endpoint should not emit search correlation:\n%s", buf.String())
	}
}

// An upload is traced like a search: as the POST it is, with the connection
// lifecycle, and without the API key, which travels in the multipart body.
func TestDebugTracesUploadAsPost(t *testing.T) {
	var buf bytes.Buffer
	srv := newTestServer(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"image_id":"img1"}`))
	})
	client := New("secret_key", WithBaseURL(srv.URL), WithTimeout(5*time.Second), WithDebug(&buf, false))

	if _, err := client.UploadImageBytes(context.Background(), png1x1, "photo.png"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"POST " + srv.URL + "/image",
		"Timeout: 5s",
		"Connecting:",
		"Using connection:",
		"Request sent, waiting for response headers",
		"Response: HTTP/1.1 200 OK",
		"Body received: 19 bytes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "GET ") || strings.Contains(out, "secret_key") {
		t.Errorf("upload traced as a GET or with the key:\n%s", out)
	}
}

package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func newDebugServer(t *testing.T, w *bytes.Buffer) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.Header().Set("Serpapi-Search-Id", "abc123")
		_, _ = rw.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	client := NewWithTimeout("secret_key", 5*time.Second)
	client.baseURL = srv.URL
	client.SetDebugWriter(w)
	return client
}

func TestDebugDisabledByDefault(t *testing.T) {
	var buf bytes.Buffer
	client := newDebugServer(t, &buf)
	client.SetDebugWriter(nil)

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no debug output, got:\n%s", buf.String())
	}
}

func TestDebugTraceCoversRequestLifecycle(t *testing.T) {
	var buf bytes.Buffer
	client := newDebugServer(t, &buf)

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

func TestDebugTraceReportsTimeout(t *testing.T) {
	var buf bytes.Buffer
	client := newSlowServer(t, 5*time.Second, 50*time.Millisecond)
	client.SetDebugWriter(&buf)

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
func newMetadataServer(t *testing.T, w *bytes.Buffer, processing time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		created := time.Now().UTC()
		processed := created.Add(processing)
		rw.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(rw, `{"search_metadata":{"id":"abc123","status":"Success","created_at":%q,"processed_at":%q,"total_time_taken":%g}}`,
			created.Format(searchMetadataTimeLayout), processed.Format(searchMetadataTimeLayout), processing.Seconds())
	}))
	t.Cleanup(srv.Close)

	client := NewWithTimeout("secret_key", 10*time.Second)
	client.baseURL = srv.URL
	client.SetDebugWriter(w)
	return client
}

func TestDebugCorrelatesSearchMetadata(t *testing.T) {
	var buf bytes.Buffer
	client := newMetadataServer(t, &buf, 1*time.Second)

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
	if !strings.Contains(out, "Note: most of the wait (+21.0s) happened before the server created the search") {
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
	if !strings.Contains(out, "Note: the server finished processing +15.0s before headers arrived") {
		t.Errorf("expected post-processing stall note:\n%s", out)
	}
}

func TestDebugNoStallNoteWhenServerProcessingDominates(t *testing.T) {
	var buf bytes.Buffer
	// Slow, but honestly slow: the engine took 40s and that's all we waited.
	tr, body := stallTracer(&buf, 0, 40*time.Second, 500*time.Millisecond)
	tr.logSearchMetadata(body)

	if strings.Contains(buf.String(), "Note:") {
		t.Errorf("slow engine should not be flagged as a client/network stall:\n%s", buf.String())
	}
}

func TestDebugSearchMetadataUnparseableIsReported(t *testing.T) {
	var buf bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"search_metadata":{"id":"abc123","status":"Success","created_at":"soon","processed_at":"later","total_time_taken":0.5}}`))
	}))
	t.Cleanup(srv.Close)
	client := NewWithTimeout("k", 5*time.Second)
	client.baseURL = srv.URL
	client.SetDebugWriter(&buf)

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
	client := newDebugServer(t, &buf)

	if _, err := client.Account(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(buf.String(), "Server search:") {
		t.Errorf("account endpoint should not emit search correlation:\n%s", buf.String())
	}
}

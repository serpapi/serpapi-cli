package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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

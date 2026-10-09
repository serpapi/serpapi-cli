package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// newSlowServer returns a client pointed at a server that waits for `delay`
// (or until the request is cancelled) before answering with a tiny JSON body.
func newSlowServer(t *testing.T, delay time.Duration, timeout time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	client := NewWithTimeout("secret_key", timeout)
	client.baseURL = srv.URL
	return client
}

func TestNewUsesDefaultTimeout(t *testing.T) {
	if got := New("k").http.Timeout; got != DefaultTimeout {
		t.Fatalf("expected default timeout %s, got %s", DefaultTimeout, got)
	}
	if DefaultTimeout < 60*time.Second {
		t.Fatalf("default timeout %s is too short for slow engines like google_ai_mode", DefaultTimeout)
	}
}

func TestTimeoutProducesActionableNetworkError(t *testing.T) {
	client := newSlowServer(t, 5*time.Second, 50*time.Millisecond)

	_, err := client.Search(context.Background(), map[string]string{"q": "x"})
	var netErr *clierrors.NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected NetworkError, got %T: %v", err, err)
	}
	for _, want := range []string{"timed out", "--timeout", "archive"} {
		if !strings.Contains(netErr.Message, want) {
			t.Errorf("message %q should mention %q", netErr.Message, want)
		}
	}
	if strings.Contains(netErr.Message, "api_key=") {
		t.Errorf("timeout message should not echo the request URL: %q", netErr.Message)
	}
}

func TestZeroTimeoutDisablesLimit(t *testing.T) {
	client := newSlowServer(t, 200*time.Millisecond, 0)
	if client.http.Timeout != 0 {
		t.Fatalf("expected no http timeout, got %s", client.http.Timeout)
	}

	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("expected slow request to succeed with timeout disabled, got %v", err)
	}
}

func TestLongerTimeoutAllowsSlowResponse(t *testing.T) {
	client := newSlowServer(t, 200*time.Millisecond, 5*time.Second)
	if _, err := client.Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatalf("expected slow request to succeed within timeout, got %v", err)
	}
}

func TestCallerCancellationIsNotReportedAsTimeout(t *testing.T) {
	client := newSlowServer(t, 5*time.Second, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := client.Search(ctx, map[string]string{"q": "x"})
	var netErr *clierrors.NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected NetworkError, got %T: %v", err, err)
	}
	if strings.Contains(netErr.Message, "timed out") {
		t.Errorf("caller cancellation should not be reported as a timeout: %q", netErr.Message)
	}
}

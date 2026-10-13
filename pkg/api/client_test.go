package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestSearchSendsParamsKeyAndUserAgent(t *testing.T) {
	var got *http.Request
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = w.Write([]byte(`{"search_metadata":{"status":"Success"}}`))
	})

	params := map[string]string{"engine": "google", "q": "coffee & tea"}
	body, err := New("secret", WithBaseURL(srv.URL+"/")).Search(context.Background(), params)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if string(body) != `{"search_metadata":{"status":"Success"}}` {
		t.Errorf("body = %s", body)
	}
	if got.URL.Path != "/search.json" {
		t.Errorf("path = %q", got.URL.Path)
	}
	q := got.URL.Query()
	if q.Get("engine") != "google" || q.Get("q") != "coffee & tea" || q.Get("api_key") != "secret" {
		t.Errorf("query = %v", q)
	}
	if !strings.HasPrefix(got.UserAgent(), "serpapi-go-cli/") {
		t.Errorf("User-Agent = %q", got.UserAgent())
	}
	if _, ok := params["api_key"]; ok {
		t.Error("Search mutated the caller's params")
	}
}

func TestSearchWithoutKeyOmitsAPIKey(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("api_key") {
			t.Errorf("unexpected api_key in %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := New("", WithBaseURL(srv.URL)).Search(context.Background(), map[string]string{"q": "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPErrorsCarryStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		message string
	}{
		{"json error message", 401, `{"error":"Invalid API key."}`, "Invalid API key."},
		{"rate limit", 429, `{"error":"Your account has run out of searches."}`, "Your account has run out of searches."},
		{"html error page", 500, `<html>oops</html>`, "HTTP 500: Internal Server Error"},
		{"empty body", 503, ``, "HTTP 503: Service Unavailable"},
		{"redirect is not followed", 302, ``, "HTTP 302: Found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "/elsewhere")
				}
				if r.URL.Path == "/elsewhere" {
					t.Error("redirect was followed")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil)
			var apiErr *clierrors.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("want APIError, got %T %v", err, err)
			}
			if apiErr.Message != tc.message || apiErr.Status != tc.status {
				t.Errorf("got (%q, %d), want (%q, %d)", apiErr.Message, apiErr.Status, tc.message, tc.status)
			}
		})
	}
}

func TestErrorInsideOKResponse(t *testing.T) {
	srv := newTestServer(t, reply(200, `{"search_metadata":{"status":"Success"},"error":"Google hasn't returned any results for this query."}`))
	_, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil)
	var apiErr *clierrors.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %v", err)
	}
	if apiErr.Status != http.StatusOK || apiErr.Message != "Google hasn't returned any results for this query." {
		t.Errorf("got (%q, %d)", apiErr.Message, apiErr.Status)
	}
}

func TestNonObjectErrorFieldIsData(t *testing.T) {
	srv := newTestServer(t, reply(200, `{"error":{"nested":true}}`))
	if _, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil); err != nil {
		t.Errorf("non-string error field should pass through, got %v", err)
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	for _, body := range []string{`<html>maintenance</html>`, `{"truncated":`, ``} {
		srv := newTestServer(t, reply(200, body))
		_, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil)
		var apiErr *clierrors.APIError
		if !errors.As(err, &apiErr) || apiErr.Message != "Unexpected non-JSON response from server" || apiErr.Status != 0 {
			t.Errorf("body %q: got %v", body, err)
		}
	}
}

func TestResponseSizeLimit(t *testing.T) {
	old := maxResponseBytes
	maxResponseBytes = 16
	t.Cleanup(func() { maxResponseBytes = old })

	srv := newTestServer(t, reply(200, `{"a":"0123456789"}`)) // 18 bytes
	_, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil)
	var apiErr *clierrors.APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "exceeded") || apiErr.Status != 0 {
		t.Fatalf("want size error, got %v", err)
	}

	srv = newTestServer(t, reply(200, `{"a":"012345678"}`)) // 17 bytes, over by one
	if _, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil); err == nil {
		t.Fatal("want size error for a body one byte over the limit")
	}

	srv = newTestServer(t, reply(200, `{"a":"01234567"}`)) // exactly 16 bytes
	if _, err := New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil); err != nil {
		t.Fatalf("body at the limit should pass: %v", err)
	}

	srv = newTestServer(t, reply(503, `{"error":"0123456789"}`)) // over the limit
	_, err = New("k", WithBaseURL(srv.URL)).Search(context.Background(), nil)
	if !errors.As(err, &apiErr) || apiErr.Status != 503 {
		t.Fatalf("an oversized error response should keep its status, got %v", err)
	}
}

func TestNetworkErrorHidesURLAndKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	_, err := New("secret-key-123", WithBaseURL(base)).Search(context.Background(), map[string]string{"q": "coffee"})
	var netErr *clierrors.NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("want NetworkError, got %T %v", err, err)
	}
	for _, leak := range []string{"secret-key-123", "api_key", "q=coffee"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaks %q: %v", leak, err)
		}
	}
	if !strings.Contains(netErr.Message, "/search.json") {
		t.Errorf("message should name the endpoint: %q", netErr.Message)
	}
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)

	_, err := New("secret", WithBaseURL(srv.URL), WithTimeout(50*time.Millisecond)).Search(context.Background(), nil)
	var netErr *clierrors.NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("want NetworkError, got %T %v", err, err)
	}
	if !strings.Contains(netErr.Message, "timed out after 50ms") || !strings.Contains(netErr.Message, "--timeout") {
		t.Errorf("message = %q", netErr.Message)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks key: %v", err)
	}
}

func TestSearchRaw(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("output") != "html" {
			t.Errorf("output param not forwarded: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("<html><body>results</body></html>"))
	})
	raw, err := New("k", WithBaseURL(srv.URL)).SearchRaw(context.Background(), map[string]string{"output": "html"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "<html><body>results</body></html>" {
		t.Errorf("raw = %q", raw)
	}
}

func TestSearchRawErrors(t *testing.T) {
	srv := newTestServer(t, reply(200, `{"error":"Unsupported output."}`))
	_, err := New("k", WithBaseURL(srv.URL)).SearchRaw(context.Background(), nil)
	var apiErr *clierrors.APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Unsupported output." {
		t.Errorf("want APIError from JSON error body, got %v", err)
	}

	srv = newTestServer(t, reply(401, `{"error":"Invalid API key."}`))
	_, err = New("k", WithBaseURL(srv.URL)).SearchRaw(context.Background(), nil)
	if !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Errorf("want APIError with status 401, got %v", err)
	}
}

func TestLocations(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/locations.json" || r.URL.Query().Has("api_key") {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`[{"name":"Austin"}]`))
	})
	body, err := New("secret", WithBaseURL(srv.URL)).Locations(context.Background(), map[string]string{"q": "austin"})
	if err != nil || string(body) != `[{"name":"Austin"}]` {
		t.Fatalf("got %s, %v", body, err)
	}

	for body, want := range map[string]string{
		`{"error":"Missing query."}`: "Missing query.",
		`{"unexpected":true}`:        "Unexpected response from locations endpoint",
	} {
		srv := newTestServer(t, reply(200, body))
		_, err := New("", WithBaseURL(srv.URL)).Locations(context.Background(), nil)
		var apiErr *clierrors.APIError
		if !errors.As(err, &apiErr) || apiErr.Message != want {
			t.Errorf("body %s: got %v, want %q", body, err, want)
		}
	}
}

func TestAccountAndArchiveRequests(t *testing.T) {
	var paths []string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(`{}`))
	})
	c := New("secret", WithBaseURL(srv.URL))
	if _, err := c.Account(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Archive(context.Background(), "abc_123-x"); err != nil {
		t.Fatal(err)
	}
	if body, err := c.ArchiveRaw(context.Background(), "abc_123-x", "rank:example.com"); err != nil || string(body) != "{}" {
		t.Fatalf("ArchiveRaw = %q, %v", body, err)
	}
	want := []string{
		"/account.json?api_key=secret",
		"/searches/abc_123-x.json?api_key=secret",
		"/searches/abc_123-x.json?api_key=secret&output=rank%3Aexample.com",
	}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestObjectEndpointsRejectOtherJSON(t *testing.T) {
	for _, body := range []string{`null`, `[{"position":1}]`, `"text"`, `42`} {
		srv := newTestServer(t, reply(200, body))
		c := New("k", WithBaseURL(srv.URL))
		calls := map[string]func() error{
			"search":  func() error { _, err := c.Search(context.Background(), nil); return err },
			"account": func() error { _, err := c.Account(context.Background()); return err },
			"archive": func() error { _, err := c.Archive(context.Background(), "abc"); return err },
		}
		for name, call := range calls {
			var apiErr *clierrors.APIError
			if err := call(); !errors.As(err, &apiErr) || apiErr.Message != "Unexpected response from server: not a JSON object" || apiErr.Status != 0 {
				t.Errorf("%s with body %s: got %v", name, body, err)
			}
		}
	}
}

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

type osTimeout struct{}

func (osTimeout) Error() string   { return "i/o timeout" }
func (osTimeout) Timeout() bool   { return true }
func (osTimeout) Temporary() bool { return true }

// With --timeout 0 there is no client limit to raise, so an operating
// system timeout, such as a TCP connect timeout, is reported as is.
func TestOSTimeoutWithoutAClientLimit(t *testing.T) {
	c := New("secret", WithTimeout(0))
	err := c.networkError("/search.json", &url.Error{Op: "Get", URL: "https://serpapi.com/search.json?api_key=secret", Err: osTimeout{}})
	var netErr *clierrors.NetworkError
	if !errors.As(err, &netErr) || netErr.Message != "Request to /search.json failed: i/o timeout" {
		t.Errorf("got %v, want the OS timeout without --timeout advice", err)
	}
}

// The advice names a --timeout above the one that just ran out.
func TestTimeoutAdviceRaisesTheLimit(t *testing.T) {
	for limit, want := range map[time.Duration]string{
		50 * time.Millisecond: "timed out after 50ms. Slow engines, such as google_ai_mode, and no_cache=true can take longer: retry with a higher --timeout, such as --timeout 120,",
		60 * time.Second:      "timed out after 60s. Slow engines, such as google_ai_mode, and no_cache=true can take longer: retry with a higher --timeout, such as --timeout 120,",
		120 * time.Second:     "--timeout 240,",
		301 * time.Second:     "--timeout 602,",
	} {
		err := New("secret", WithTimeout(limit)).networkError("/search.json", osTimeout{})
		var netErr *clierrors.NetworkError
		if !errors.As(err, &netErr) || !strings.Contains(netErr.Message, want) {
			t.Errorf("limit %s: got %v, want it to contain %q", limit, err, want)
		}
	}
}

// The timeout advice points to --debug only while the trace is off.
func TestTimeoutAdvicePointsToDebugWhenItIsOff(t *testing.T) {
	for _, debug := range []bool{false, true} {
		c := New("secret", WithTimeout(time.Second))
		if debug {
			WithDebug(io.Discard, false)(c)
		}
		var netErr *clierrors.NetworkError
		if err := c.networkError("/search.json", osTimeout{}); !errors.As(err, &netErr) {
			t.Fatalf("debug %v: got %v, want a NetworkError", debug, err)
		}
		if suggests := strings.Contains(netErr.Message, "--debug shows which step stalled"); suggests == debug {
			t.Errorf("debug %v: message %q", debug, netErr.Message)
		}
	}
}

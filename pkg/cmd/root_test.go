package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/output"
)

func TestResolveDebug(t *testing.T) {
	tests := []struct {
		name string
		flag bool
		env  string
		want bool
	}{
		{name: "default off"},
		{name: "flag on", flag: true, want: true},
		{name: "env 1", env: "1", want: true},
		{name: "env true", env: "true", want: true},
		{name: "env 0", env: "0"},
		{name: "env false", env: "FALSE"},
		{name: "env off", env: "off"},
		{name: "env blank", env: "  "},
		{name: "flag wins over env 0", flag: true, env: "0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			debugFlag = tt.flag
			t.Cleanup(func() { debugFlag = false })
			t.Setenv("SERPAPI_DEBUG", tt.env)

			if got := resolveDebug(); got != tt.want {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

const debugSearchBody = `{"search_metadata":{"id":"abc123","status":"Success","created_at":"2026-10-09 12:00:00 UTC","processed_at":"2026-10-09 12:00:01 UTC","total_time_taken":1},"organic_results":[]}`

// TestDebugOffTerminalKeepsStderrJSONLines is the reviewer's case: scripts
// and agents parse stderr line by line, so a piped --debug run must emit
// only JSON objects there, and the result on stdout must be untouched.
func TestDebugOffTerminalKeepsStderrJSONLines(t *testing.T) {
	fakeAPI(t, respond(debugSearchBody))

	res := execute(t, "", "--debug", "search", "engine=google", "q=coffee")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", res.code, res.stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, res.stdout)
	}

	lines := strings.Split(strings.TrimSpace(res.stderr), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected a trace on stderr, got %d lines:\n%s", len(lines), res.stderr)
	}
	var events []string
	for _, line := range lines {
		var v struct {
			Debug *struct {
				At      string  `json:"at"`
				Elapsed float64 `json:"elapsed"`
				Event   string  `json:"event"`
			} `json:"debug"`
			Error   map[string]any `json:"error"`
			Warning map[string]any `json:"warning"`
		}
		if err := json.Unmarshal([]byte(line), &v); err != nil || (v.Debug == nil && v.Error == nil && v.Warning == nil) {
			t.Fatalf("stderr line is not a JSON debug, warning or error object: %q", line)
		}
		if v.Error != nil {
			t.Fatalf("unexpected error on stderr: %v", v.Error)
		}
		if v.Debug != nil {
			events = append(events, v.Debug.Event)
		}
	}
	joined := strings.Join(events, "\n")
	for _, want := range []string{"GET http", "Timeout: 60s", "Request sent", "Response: HTTP/1.1 200 OK", "Server search: id=abc123", "Server timeline:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("trace missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(res.stderr, "[debug]") {
		t.Errorf("piped stderr must not carry plain-text debug lines:\n%s", res.stderr)
	}
}

func TestDebugOnTerminalPrintsPlainText(t *testing.T) {
	fakeAPI(t, respond(debugSearchBody))
	isTerminal := output.IsTerminal
	output.IsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { output.IsTerminal = isTerminal })

	res := execute(t, "", "--debug", "search", "engine=google", "q=coffee")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", res.code, res.stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(res.stderr), "\n") {
		if !strings.HasPrefix(line, "[debug] ") {
			t.Errorf("terminal stderr line should be plain text, got %q", line)
		}
	}
	if !strings.Contains(res.stderr, "Server timeline:") {
		t.Errorf("expected server timeline in trace:\n%s", res.stderr)
	}
}

func TestDebugEnvEnablesTrace(t *testing.T) {
	fakeAPI(t, respond(debugSearchBody))
	t.Setenv("SERPAPI_DEBUG", "1")

	res := execute(t, "", "search", "engine=google", "q=coffee")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, `"debug":`) {
		t.Errorf("SERPAPI_DEBUG=1 should enable the trace:\n%s", res.stderr)
	}
}

func TestNoDebugMeansNoTrace(t *testing.T) {
	fakeAPI(t, respond(debugSearchBody))

	res := execute(t, "", "search", "engine=google", "q=coffee")

	wantWarnings(t, wantOK(t, res))
	if strings.Contains(res.stderr, "debug") {
		t.Errorf("stderr should be silent without --debug:\n%s", res.stderr)
	}
}

func TestDebugTimeoutErrorStaysLastJSONLine(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})

	res := execute(t, "", "--debug", "--timeout", "0.05", "search", "engine=google", "q=coffee")

	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", res.code, res.stderr)
	}
	lines := strings.Split(strings.TrimSpace(res.stderr), "\n")
	var last struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil || last.Error == nil {
		t.Fatalf("last stderr line should be the JSON error, got %q", lines[len(lines)-1])
	}
	if msg, _ := last.Error["message"].(string); strings.Contains(msg, "--debug") || !strings.Contains(msg, "timed out") {
		t.Errorf("with --debug on, the timeout message shouldn't suggest it: %q", msg)
	}
	if !strings.Contains(res.stderr, "Request failed:") {
		t.Errorf("trace should report the failed request:\n%s", res.stderr)
	}
}

// TestDebugTraceNeverPrintsTheKey covers responses that carry the key back:
// redirects the client refuses to follow, since the request URL carries it,
// one echoing the query and a proxy's login page with the original URL
// encoded in its own, and a header named after it.
func TestDebugTraceNeverPrintsTheKey(t *testing.T) {
	key := strings.Repeat("0123456789abcdef", 4)
	responses := []struct {
		name    string
		headers func(r *http.Request) map[string]string
	}{
		{"echoed query", func(r *http.Request) map[string]string {
			return map[string]string{"Location": "https://serpapi.com/search.json?" + r.URL.RawQuery}
		}},
		{"encoded in a login URL", func(r *http.Request) map[string]string {
			return map[string]string{"Location": "https://proxy.example/login?origurl=" + url.QueryEscape("https://serpapi.com"+r.URL.RequestURI())}
		}},
		{"a header named after it", func(*http.Request) map[string]string { return map[string]string{"X-Echo-" + key: "1"} }},
	}
	for _, response := range responses {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/terminal=%v", response.name, terminal), func(t *testing.T) {
				fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
					for name, value := range response.headers(r) {
						w.Header().Set(name, value)
					}
					w.WriteHeader(http.StatusFound)
				})
				isTerminal := output.IsTerminal
				output.IsTerminal = func(io.Writer) bool { return terminal }
				t.Cleanup(func() { output.IsTerminal = isTerminal })

				res := execute(t, "", "--api-key", key, "--debug", "search", "engine=google", "q=coffee")

				if res.code != 1 {
					t.Fatalf("exit code = %d, want 1; stderr: %s", res.code, res.stderr)
				}
				if n := strings.Count(res.stderr, key); n > 0 {
					t.Errorf("stderr prints the API key %d times:\n%s", n, res.stderr)
				}
				if !strings.Contains(res.stderr, "Response: HTTP/1.1 302 Found") {
					t.Errorf("trace should still show the response:\n%s", res.stderr)
				}
				for _, line := range strings.Split(strings.TrimSpace(res.stderr), "\n") {
					if !terminal && !json.Valid([]byte(line)) {
						t.Errorf("piped stderr line is not JSON: %q", line)
					}
				}
			})
		}
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: api.DefaultTimeout},
		{name: "flag seconds", flag: "120", want: 120 * time.Second},
		{name: "flag fractional", flag: "1.5", want: 1500 * time.Millisecond},
		{name: "flag zero disables", flag: "0", want: 0},
		{name: "env fallback", env: "90", want: 90 * time.Second},
		{name: "flag overrides env", flag: "10", env: "90", want: 10 * time.Second},
		{name: "negative rejected", flag: "-5", wantErr: true},
		{name: "garbage rejected", flag: "soon", wantErr: true},
		{name: "env garbage rejected", env: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeoutFlag = tt.flag
			t.Cleanup(func() { timeoutFlag = "" })
			t.Setenv("SERPAPI_TIMEOUT", tt.env)

			got, err := resolveTimeout()
			if tt.wantErr {
				var ue *clierrors.UsageError
				if !errors.As(err, &ue) {
					t.Fatalf("expected UsageError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

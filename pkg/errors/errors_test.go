package errors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/serpapi/serpapi-cli/pkg/output"
)

func TestRedactNoAPIKey(t *testing.T) {
	s := "some error without credentials"
	result := RedactAPIKey(s)
	if result != s {
		t.Errorf("expected no change, got %q", result)
	}
}

func TestRedactSingleOccurrence(t *testing.T) {
	s := "https://serpapi.com/search.json?q=coffee&api_key=supersecret&engine=google"
	result := RedactAPIKey(s)
	if !strings.Contains(result, "api_key=[REDACTED]") {
		t.Errorf("expected redacted key, got %q", result)
	}
	if strings.Contains(result, "supersecret") {
		t.Errorf("secret not redacted in %q", result)
	}
}

func TestRedactMultipleOccurrences(t *testing.T) {
	s := "api_key=first&other=x&api_key=second"
	result := RedactAPIKey(s)
	if strings.Contains(result, "first") || strings.Contains(result, "second") {
		t.Errorf("secrets not redacted in %q", result)
	}
	if count := strings.Count(result, "[REDACTED]"); count != 2 {
		t.Errorf("expected 2 redactions, got %d in %q", count, result)
	}
}

func TestRedactAtEndOfString(t *testing.T) {
	s := "request failed: api_key=abc123"
	result := RedactAPIKey(s)
	if result != "request failed: api_key=[REDACTED]" {
		t.Errorf("expected 'request failed: api_key=[REDACTED]', got %q", result)
	}
}

func TestRedactAlreadyRedactedNoLoop(t *testing.T) {
	s := "api_key=[REDACTED]"
	result := RedactAPIKey(s)
	if result != "api_key=[REDACTED]" {
		t.Errorf("expected no change on already-redacted, got %q", result)
	}
}

func TestPrintErrorUsageError(t *testing.T) {
	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	PrintError(&UsageError{Message: "test error"})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("failed to parse error JSON: %v\noutput: %s", err, output)
	}
	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", parsed["error"])
	}
	if errObj["code"] != "usage_error" {
		t.Errorf("expected code usage_error, got %v", errObj["code"])
	}
	if errObj["message"] != "test error" {
		t.Errorf("expected message 'test error', got %v", errObj["message"])
	}
}

func TestExitCodeUsageError(t *testing.T) {
	if code := ExitCode(&UsageError{Message: "x"}); code != 2 {
		t.Errorf("expected 2, got %d", code)
	}
}

func TestExitCodeAPIError(t *testing.T) {
	if code := ExitCode(&APIError{Message: "x"}); code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestExitCodeWrappedError(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", &APIError{Message: "inner"})
	if code := ExitCode(wrapped); code != 1 {
		t.Errorf("expected 1 for wrapped APIError, got %d", code)
	}
}

func TestUnwrapAPIError(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := &NetworkError{Message: "failed", Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected Unwrap to expose cause")
	}
}

func TestExitCodeNetworkError(t *testing.T) {
	if code := ExitCode(&NetworkError{Message: "x"}); code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func decodeError(t *testing.T, err error) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	FprintError(&buf, err)
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected exactly one line of output, got %q", out)
	}
	var parsed struct {
		Error map[string]any `json:"error"`
	}
	if jsonErr := json.Unmarshal(buf.Bytes(), &parsed); jsonErr != nil {
		t.Fatalf("invalid error JSON %q: %v", out, jsonErr)
	}
	return parsed.Error
}

func TestWriteErrorFields(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    string
		message string
		status  any
	}{
		{"api error with HTTP status", &APIError{Message: "Invalid API key.", Status: 401}, "api_error", "Invalid API key.", float64(401)},
		{"api error inside a 200 response", &APIError{Message: "No results.", Status: 200}, "api_error", "No results.", float64(200)},
		{"api error without a usable answer", &APIError{Message: "Unexpected non-JSON response from server"}, "api_error", "Unexpected non-JSON response from server", nil},
		{"wrapped network error", fmt.Errorf("ctx: %w", &NetworkError{Message: "timed out"}), "network_error", "timed out", nil},
		{"wrapped usage error", fmt.Errorf("ctx: %w", &UsageError{Message: "bad flag"}), "usage_error", "bad flag", nil},
		{"untyped error", errors.New("boom"), "api_error", "boom", nil},
		{"cobra usage error", errors.New(`unknown command "x" for "serpapi"`), "usage_error", `unknown command "x" for "serpapi"`, nil},
		{"redacts api key", &NetworkError{Message: "GET /search?api_key=secret123&q=x failed"}, "network_error", "GET /search?api_key=[REDACTED]&q=x failed", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeError(t, tc.err)
			if got["code"] != tc.code {
				t.Errorf("code = %v, want %v", got["code"], tc.code)
			}
			if got["message"] != tc.message {
				t.Errorf("message = %v, want %v", got["message"], tc.message)
			}
			if got["status"] != tc.status {
				t.Errorf("status = %v, want %v", got["status"], tc.status)
			}
		})
	}
}

func TestWriteErrorKeepsHTMLCharacters(t *testing.T) {
	got := decodeError(t, &APIError{Message: "Use <q> & <engine>"})
	if got["message"] != "Use <q> & <engine>" {
		t.Errorf("message = %v", got["message"])
	}
}

func TestPrintWarningIsOneRedactedJSONLine(t *testing.T) {
	var buf bytes.Buffer
	PrintWarning(&buf, "page 2 failed: GET /search?api_key=secret & more")
	line := buf.String()
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("want exactly one line, got %q", line)
	}
	var got struct {
		Warning struct {
			Message string `json:"message"`
		} `json:"warning"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if strings.Contains(got.Warning.Message, "secret") || !strings.Contains(got.Warning.Message, "& more") {
		t.Errorf("message = %q", got.Warning.Message)
	}
}

func FuzzFprintErrorIsOneRedactedJSONLine(f *testing.F) {
	f.Add("request failed: ", "abc123def456", "&q=coffee")
	f.Add("api_key=", "0123456789abcdef", "\nsecond line")
	f.Add("x\"y\\", "SECRETSECRET", "' end")
	f.Fuzz(func(t *testing.T, prefix, secret, suffix string) {
		msg := prefix + "api_key=" + secret + suffix
		var buf bytes.Buffer
		FprintError(&buf, &APIError{Message: msg, Status: 500})
		out := buf.String()
		if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
			t.Fatalf("not one line: %q", out)
		}
		var parsed struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("not JSON: %q: %v", out, err)
		}
		if utf8.ValidString(msg) && parsed.Error.Message != RedactAPIKey(msg) {
			t.Fatalf("message %q, want %q", parsed.Error.Message, RedactAPIKey(msg))
		}
		if len(secret) >= 8 && isAlnum(secret) && !strings.Contains(prefix+suffix, secret) && strings.Contains(parsed.Error.Message, secret) {
			t.Fatalf("secret %q leaked in %q", secret, parsed.Error.Message)
		}
	})
}

func isAlnum(s string) bool {
	for _, r := range s {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			return false
		}
	}
	return true
}

// gojq errors quote response values, such as a page title, so stderr gets
// the same terminal sanitizing as stdout, and the JSON still decodes to the
// original message.
func TestTerminalStderrIsSanitized(t *testing.T) {
	isTerminal := output.IsTerminal
	output.IsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { output.IsTerminal = isTerminal })

	msg := "expected an object but got: string (\"invoice\u202etxt.exe \u009b31m red\x7f\")"
	for name, print := range map[string]func(io.Writer){
		"error":   func(w io.Writer) { FprintError(w, &UsageError{Message: msg}) },
		"warning": func(w io.Writer) { PrintWarning(w, msg) },
	} {
		var buf bytes.Buffer
		print(&buf)
		if strings.ContainsAny(buf.String(), "\u202e\u009b\x7f") {
			t.Errorf("%s: raw control characters reached the terminal: %q", name, buf.String())
		}
		var got map[string]struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("%s: not JSON: %v", name, err)
		}
		if got[name].Message != msg {
			t.Errorf("%s: message = %q, want %q", name, got[name].Message, msg)
		}
	}
}

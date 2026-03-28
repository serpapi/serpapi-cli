package errors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
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

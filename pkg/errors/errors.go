package errors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// UsageError indicates incorrect CLI usage (exit code 2).
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return "Usage error: " + e.Message }

// APIError indicates an error returned by the SerpApi service (exit code 1).
type APIError struct {
	Message string
	Cause   error
}

func (e *APIError) Error() string { return "API error: " + e.Message }
func (e *APIError) Unwrap() error { return e.Cause }

// NetworkError indicates a connectivity or timeout failure (exit code 1).
type NetworkError struct {
	Message string
	Cause   error
}

func (e *NetworkError) Error() string { return "Network error: " + e.Message }
func (e *NetworkError) Unwrap() error { return e.Cause }

// ExitCode returns the appropriate process exit code for the given error.
func ExitCode(err error) int {
	var ue *UsageError
	if errors.As(err, &ue) {
		return 2
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return 1
	}
	var ne *NetworkError
	if errors.As(err, &ne) {
		return 1
	}
	if isCobraUsageError(err) {
		return 2
	}
	return 1
}

func errorCode(err error) string {
	var ue *UsageError
	if errors.As(err, &ue) {
		return "usage_error"
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return "api_error"
	}
	var ne *NetworkError
	if errors.As(err, &ne) {
		return "network_error"
	}
	if isCobraUsageError(err) {
		return "usage_error"
	}
	return "api_error"
}

// isCobraUsageError detects cobra argument-validation errors by message pattern.
// Cobra does not export typed errors for argument/flag validation failures,
// so we match on known message prefixes. SetFlagErrorFunc in root.go handles
// flag errors; this catches the remaining argument-validation messages.
func isCobraUsageError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "accepts ") ||
		strings.HasPrefix(msg, "requires ") ||
		strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "required flag") ||
		strings.HasPrefix(msg, "invalid argument")
}

// PrintError writes a structured JSON error to stderr.
func PrintError(err error) {
	code := errorCode(err)
	message := RedactAPIKey(err.Error())

	// Strip the "Usage error: " / "API error: " / "Network error: " prefix
	// that the Error() method adds, so the JSON message stays clean.
	switch e := err.(type) {
	case *UsageError:
		message = RedactAPIKey(e.Message)
	case *APIError:
		message = RedactAPIKey(e.Message)
	case *NetworkError:
		message = RedactAPIKey(e.Message)
	}

	payload := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if jsonErr := enc.Encode(payload); jsonErr != nil {
		// Fallback if JSON encoding fails
		fmt.Fprintf(os.Stderr, "{\"error\":{\"code\":\"%s\",\"message\":\"%s\"}}\n",
			code, strings.ReplaceAll(strings.ReplaceAll(message, "\\", "\\\\"), "\"", "\\\""))
		return
	}
	fmt.Fprint(os.Stderr, buf.String())
}

// RedactAPIKey replaces api_key values in s with [REDACTED].
func RedactAPIKey(s string) string {
	const prefix = "api_key="
	if !strings.Contains(s, prefix) {
		return s
	}
	out := s
	searchFrom := 0
	for {
		idx := strings.Index(out[searchFrom:], prefix)
		if idx < 0 {
			break
		}
		pos := searchFrom + idx
		valueStart := pos + len(prefix)
		valueEnd := len(out)
		for i := valueStart; i < len(out); i++ {
			if out[i] == '&' || out[i] == ' ' || out[i] == ')' || out[i] == '"' || out[i] == '\'' {
				valueEnd = i
				break
			}
		}
		out = out[:valueStart] + "[REDACTED]" + out[valueEnd:]
		searchFrom = valueStart + len("[REDACTED]")
	}
	return out
}

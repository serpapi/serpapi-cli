package errors

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/serpapi/serpapi-cli/pkg/output"
)

// UsageError indicates incorrect CLI usage (exit code 2).
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return "Usage error: " + e.Message }

// APIError indicates an error returned by the SerpApi service (exit code 1).
type APIError struct {
	Message string
	// Status is the HTTP status of the response: 200 when the API reported the
	// error inside a successful response, 0 when there was no usable answer,
	// such as a 200 with a body that is not JSON.
	Status int
	Cause  error
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

// InterruptedError means Ctrl-C or SIGTERM stopped the CLI (exit code 130).
type InterruptedError struct{ Message string }

func (e *InterruptedError) Error() string { return "Interrupted: " + e.Message }

// ExitCode returns the appropriate process exit code for the given error.
func ExitCode(err error) int {
	_, exit := classify(err)
	return exit
}

func classify(err error) (code string, exit int) {
	var ue *UsageError
	var ae *APIError
	var ne *NetworkError
	var ie *InterruptedError
	switch {
	case errors.As(err, &ie):
		return "interrupted", 130
	case errors.As(err, &ue):
		return "usage_error", 2
	case errors.As(err, &ae):
		return "api_error", 1
	case errors.As(err, &ne):
		return "network_error", 1
	case isCobraUsageError(err):
		return "usage_error", 2
	}
	return "api_error", 1
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

// PrintError writes err to stderr; see FprintError.
func PrintError(err error) {
	FprintError(os.Stderr, err)
}

// FprintError writes err to w as one line of JSON:
//
//	{"error":{"code":"api_error","message":"...","status":429}}
//
// status is the HTTP status of the API's answer, 200 when a successful
// response reported an error, and absent when there was no usable answer.
func FprintError(w io.Writer, err error) {
	code, _ := classify(err)
	body := struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status,omitempty"`
	}{Code: code, Message: err.Error()}

	var ue *UsageError
	var ae *APIError
	var ne *NetworkError
	var ie *InterruptedError
	switch {
	case errors.As(err, &ie):
		body.Message = ie.Message
	case errors.As(err, &ue):
		body.Message = ue.Message
	case errors.As(err, &ae):
		body.Message, body.Status = ae.Message, ae.Status
	case errors.As(err, &ne):
		body.Message = ne.Message
	}
	body.Message = RedactAPIKey(body.Message)
	writeJSONLine(w, map[string]any{"error": body})
}

// PrintWarning writes a non-fatal problem to w as one line of JSON, so stderr
// stays parseable line by line:
//
//	{"warning":{"message":"..."}}
func PrintWarning(w io.Writer, message string) {
	writeJSONLine(w, map[string]any{"warning": map[string]string{"message": RedactAPIKey(message)}})
}

func writeJSONLine(w io.Writer, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	b := buf.Bytes()
	if output.IsTerminal(w) {
		b = output.ForTerminal(b)
	}
	_, _ = w.Write(b)
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

package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintJQValueString(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue("hello", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestPrintJQValueNumber(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue(42.0, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != "42" {
		t.Errorf("expected '42', got %q", got)
	}
}

func TestPrintJQValueBool(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue(true, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != "true" {
		t.Errorf("expected 'true', got %q", got)
	}
}

func TestPrintJQValueNull(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue(nil, &buf)
	if err != nil {
		t.Fatal(err)
	}
	// Null produces an empty line (just newline)
	if buf.String() != "\n" {
		t.Errorf("expected just newline, got %q", buf.String())
	}
}

func TestPrintJQValueObject(t *testing.T) {
	var buf bytes.Buffer
	obj := map[string]any{"key": "value"}
	err := PrintJQValue(obj, &buf)
	if err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "key") || !strings.Contains(output, "value") {
		t.Errorf("expected JSON object, got %q", output)
	}
}

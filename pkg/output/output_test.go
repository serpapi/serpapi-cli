package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintJQValueString(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue("hello", &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestPrintJQValueNumber(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue(42.0, &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != "42" {
		t.Errorf("expected '42', got %q", got)
	}
}

func TestPrintJQValueBool(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue(true, &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != "true" {
		t.Errorf("expected 'true', got %q", got)
	}
}

func TestPrintJQValueNull(t *testing.T) {
	var buf bytes.Buffer
	err := PrintJQValue(nil, &buf, false)
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
	err := PrintJQValue(obj, &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "key") || !strings.Contains(output, "value") {
		t.Errorf("expected JSON object, got %q", output)
	}
}

func TestPrintJSONIndentsWhenNotATerminal(t *testing.T) {
	var buf bytes.Buffer
	in := `{"b":1,  "a":"<x> & y","c":[1,{"d":2}]}`
	if err := PrintJSON(&buf, []byte(in)); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"b\": 1,\n  \"a\": \"<x> & y\",\n  \"c\": [\n    1,\n    {\n      \"d\": 2\n    }\n  ]\n}\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestPrintRaw(t *testing.T) {
	for in, want := range map[string]string{"": "", "# Title": "# Title\n", "a\n": "a\n"} {
		var buf bytes.Buffer
		if err := PrintRaw(&buf, []byte(in)); err != nil {
			t.Fatal(err)
		}
		if buf.String() != want {
			t.Errorf("PrintRaw(%q) = %q, want %q", in, buf.String(), want)
		}
	}
}

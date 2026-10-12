package output

import (
	"bytes"
	"encoding/json"
	"reflect"
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

func TestPrintJSONKeepsInvalidInput(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintJSON(&buf, []byte("not json\n")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "not json\n" {
		t.Errorf("got %q", buf.String())
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

func FuzzPrintJSONPiped(f *testing.F) {
	f.Add([]byte(`{"a": [1, 2.50, 1e400, 12345678901234567890], "b": "<&> café \u2028"}`))
	f.Add([]byte(`  null `))
	f.Add([]byte(`{"truncated":`))
	f.Add([]byte("{\"\":0}\n\u0085"))
	f.Add([]byte("{\"a\":1}\u00a0"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var buf bytes.Buffer
		if err := PrintJSON(&buf, data); err != nil {
			t.Fatal(err)
		}
		out := buf.Bytes()
		if !json.Valid(data) {
			if want := append(bytes.TrimSpace(data), '\n'); !bytes.Equal(out, want) {
				t.Fatalf("invalid JSON %q changed to %q", data, out)
			}
			return
		}
		var want bytes.Buffer
		if err := json.Indent(&want, bytes.TrimRight(data, " \t\r\n"), "", "  "); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, append(want.Bytes(), '\n')) {
			t.Fatalf("piped output is not the indented input: %q -> %q", data, out)
		}
		if a, b := decodeNumbers(t, data), decodeNumbers(t, out); !reflect.DeepEqual(a, b) {
			t.Fatalf("indenting changed the value: %q -> %q", data, out)
		}
	})
}

func decodeNumbers(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

package output

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func asTerminal(t *testing.T) {
	t.Helper()
	old := IsTerminal
	IsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { IsTerminal = old })
}

func TestForTerminal(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"ordinary unicode", "Café ☕ 東京 🙂 مرحبا", "Café ☕ 東京 🙂 مرحبا"},
		{"tab and newline", "a\tb\nc", "a\tb\nc"},
		{"CSI clears the screen", "x\x1b[2Jy", `x\u001b[2Jy`},
		{"OSC 52 sets the clipboard", "\x1b]52;c;ZXZpbA==\x07", `\u001b]52;c;ZXZpbA==\u0007`},
		{"OSC 8 hyperlink", "\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\", `\u001b]8;;https://evil.example\u001b\click\u001b]8;;\u001b\`},
		{"C1 CSI", "a\u009b31mb", `a\u009b31mb`},
		{"DEL", "a\x7fb", `a\u007fb`},
		{"CRLF becomes LF", "a\r\nb\r\n", "a\nb\n"},
		{"lone CR", "abc\rX", `abc\u000dX`},
		{"NUL", "a\x00b", `a\u0000b`},
		{"bidi override", "invoice\u202Efdp.exe", `invoice\u202efdp.exe`},
		{"bidi isolate", "a\u2066b\u2069", `a\u2066b\u2069`},
		{"invalid UTF-8", "a\xffb\xc2", "a\uFFFDb\uFFFD"},
		{"literal U+FFFD stays", "a\uFFFDb", "a\uFFFDb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(ForTerminal([]byte(tc.in))); got != tc.want {
				t.Errorf("ForTerminal(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestForTerminalReturnsCleanInputAsIs(t *testing.T) {
	in := []byte(`{"title":"Café","link":"https://example.com"}`)
	if out := ForTerminal(in); &out[0] != &in[0] {
		t.Error("clean input was copied")
	}
}

func TestTerminalOutputIsSanitized(t *testing.T) {
	asTerminal(t)
	t.Setenv("NO_COLOR", "")
	hostile := "Shop\x1b]52;c;cGF3bmVk\x07 \u009b2J"

	var raw bytes.Buffer
	if err := PrintRaw(&raw, []byte("# "+hostile+"\r\n")); err != nil {
		t.Fatal(err)
	}
	if got, want := raw.String(), "# Shop\\u001b]52;c;cGF3bmVk\\u0007 \\u009b2J\n"; got != want {
		t.Errorf("PrintRaw = %q, want %q", got, want)
	}

	var jqOut bytes.Buffer
	if err := PrintJQValue(hostile, &jqOut, true); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(jqOut.String(), "\x1b\x07\u009b") {
		t.Errorf("jq string kept control characters: %q", jqOut.String())
	}
	jqOut.Reset()
	if err := PrintJQValue(map[string]any{"t": "a\x7fb\u009b"}, &jqOut, true); err != nil {
		t.Fatal(err)
	}
	if got := jqOut.String(); got != "{\"t\":\"a\\u007fb\\u009b\"}\n" {
		t.Errorf("jq object = %q", got)
	}

	var js bytes.Buffer
	if err := PrintJSON(&js, []byte("{\"title\":\"a\u009bb\x7f\"}")); err != nil {
		t.Fatal(err)
	}
	out := js.String()
	if strings.Contains(out, "\u009b") || strings.Contains(out, "\x7f") {
		t.Errorf("colored JSON kept C1 or DEL: %q", out)
	}
	if !strings.Contains(out, "\x1b[34m") {
		t.Errorf("colored JSON lost its own colors: %q", out)
	}
}

func TestPipedOutputIsExact(t *testing.T) {
	hostile := "a\x1b[2Jb\u009b\r\n"
	var raw, jqOut bytes.Buffer
	if err := PrintRaw(&raw, []byte(hostile)); err != nil {
		t.Fatal(err)
	}
	if err := PrintJQValue(hostile, &jqOut, false); err != nil {
		t.Fatal(err)
	}
	if raw.String() != hostile || jqOut.String() != hostile+"\n" {
		t.Errorf("piped output changed: %q, %q", raw.String(), jqOut.String())
	}
}

func FuzzTerminalJSONKeepsValue(f *testing.F) {
	f.Add([]byte("{\"a\":\"\u009b[2J\x7f\",\"b\":[1,\"\u202e\"]}"))
	f.Add([]byte(`{"t":"\u001b]52;c;eA==\u0007"}`))
	f.Add([]byte("[\"\xff\xfe\",null]"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if !json.Valid(data) {
			return
		}
		old := IsTerminal
		IsTerminal = func(io.Writer) bool { return true }
		defer func() { IsTerminal = old }()
		t.Setenv("NO_COLOR", "1")
		var buf bytes.Buffer
		if err := PrintJSON(&buf, data); err != nil {
			t.Fatal(err)
		}
		out := buf.Bytes()
		for i := 0; i < len(out); {
			r, size := utf8.DecodeRune(out[i:])
			if unsafeForTerminal(out, i, r, size) {
				t.Fatalf("terminal output keeps %U at %d: %q", r, i, out)
			}
			i += size
		}
		var before, after any
		if err := json.Unmarshal(data, &before); err != nil {
			return
		}
		if err := json.Unmarshal(out, &after); err != nil {
			t.Fatalf("terminal output is not JSON: %v: %q", err, out)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("terminal output changed the value: %q -> %q", data, out)
		}
	})
}

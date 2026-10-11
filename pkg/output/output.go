package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/tidwall/pretty"
	"golang.org/x/term"
)

// darkStyle matches stripe-cli's darkTerminalStyle exactly.
var darkStyle = &pretty.Style{
	Key:    [2]string{"\x1B[34m", "\x1B[0m"}, // blue
	String: [2]string{"\x1B[32m", "\x1B[0m"}, // green
	Number: [2]string{"\x1B[33m", "\x1B[0m"}, // yellow
	True:   [2]string{"\x1B[35m", "\x1B[0m"}, // magenta
	False:  [2]string{"\x1B[35m", "\x1B[0m"}, // magenta
	Null:   [2]string{"\x1B[31m", "\x1B[0m"}, // red
}

// IsTerminal reports whether w is a terminal. Tests replace it.
var IsTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// SameStream reports whether a and b write to the same file or pipe, as
// stdout and stderr do after 2>&1.
func SameStream(a, b io.Writer) bool {
	fa, okA := a.(*os.File)
	fb, okB := b.(*os.File)
	if !okA || !okB {
		return false
	}
	sa, errA := fa.Stat()
	sb, errB := fb.Stat()
	return errA == nil && errB == nil && os.SameFile(sa, sb)
}

// PrintJSON writes a JSON document to w as FormatJSON formats it.
func PrintJSON(w io.Writer, data []byte) error {
	_, err := w.Write(FormatJSON(w, data))
	return err
}

// FormatJSON returns a JSON document as it is shown on w, preserving key
// order. A terminal gets it indented, through ForTerminal, and colored
// (unless NO_COLOR is set); anything else, such as a pipe, gets it indented as
// tidwall/pretty formats it.
func FormatJSON(w io.Writer, data []byte) []byte {
	if IsTerminal(w) {
		out := ForTerminal(pretty.Pretty(data))
		if os.Getenv("NO_COLOR") == "" {
			out = pretty.Color(out, darkStyle)
		}
		return out
	}
	return pretty.Pretty(data)
}

// PrintRaw writes a non-JSON response body (Markdown, HTML, CSV) to w as is,
// ending it with a newline. A terminal gets it through ForTerminal.
func PrintRaw(w io.Writer, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	if IsTerminal(w) {
		body = ForTerminal(body)
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if body[len(body)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

// ForTerminal makes text from search results safe to show on a terminal,
// which acts on control characters: a page title holding ESC ] 52 could set
// the clipboard, ESC [ 2J clears the screen. It writes as \u00XX every C0
// control but tab and newline, DEL and the C1 controls, writes bidirectional
// override and isolate characters as \u20XX, drops the CR of a CRLF and turns
// invalid UTF-8 into U+FFFD. The escapes are valid inside JSON strings.
func ForTerminal(b []byte) []byte {
	i := 0
	for i < len(b) {
		r, size := utf8.DecodeRune(b[i:])
		if unsafeForTerminal(b, i, r, size) {
			break
		}
		i += size
	}
	if i == len(b) {
		return b
	}
	out := bytes.NewBuffer(make([]byte, 0, len(b)+16))
	out.Write(b[:i])
	for i < len(b) {
		r, size := utf8.DecodeRune(b[i:])
		switch {
		case r == '\r' && i+1 < len(b) && b[i+1] == '\n':
		case r == utf8.RuneError && size == 1:
			out.WriteRune(utf8.RuneError)
		case unsafeForTerminal(b, i, r, size):
			fmt.Fprintf(out, `\u%04x`, r)
		default:
			out.Write(b[i : i+size])
		}
		i += size
	}
	return out.Bytes()
}

func unsafeForTerminal(b []byte, i int, r rune, size int) bool {
	switch {
	case r == '\t' || r == '\n':
		return false
	case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == utf8.RuneError && size == 1
}

// PrintJQValue prints a single jq result value with raw scalar output.
// Strings are unquoted. Numbers and bools are printed as-is.
// Null produces an empty line. Objects and arrays are JSON-encoded.
// terminal sends strings and JSON through ForTerminal.
func PrintJQValue(v any, w io.Writer, terminal bool) error {
	switch val := v.(type) {
	case string:
		if terminal {
			val = string(ForTerminal([]byte(val)))
		}
		_, err := fmt.Fprintln(w, val)
		return err
	case bool:
		_, err := fmt.Fprintln(w, val)
		return err
	case nil:
		_, err := fmt.Fprintln(w)
		return err
	case json.Number:
		_, err := fmt.Fprintln(w, val.String())
		return err
	case float64:
		_, err := fmt.Fprintln(w, formatNumber(val))
		return err
	case int:
		_, err := fmt.Fprintln(w, val)
		return err
	default:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(val); err != nil {
			return err
		}
		out := buf.Bytes()
		if terminal {
			out = ForTerminal(out)
		}
		_, err := w.Write(out)
		return err
	}
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

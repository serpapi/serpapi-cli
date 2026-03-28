package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/tidwall/pretty"
	"golang.org/x/term"
)

// darkStyle matches stripe-cli's darkTerminalStyle exactly.
var darkStyle = &pretty.Style{
	Key:    [2]string{"\x1B[34m", "\x1B[0m"},   // blue
	String: [2]string{"\x1B[32m", "\x1B[0m"},   // green
	Number: [2]string{"\x1B[33m", "\x1B[0m"},   // yellow
	True:   [2]string{"\x1B[35m", "\x1B[0m"},   // magenta
	False:  [2]string{"\x1B[35m", "\x1B[0m"},   // magenta
	Null:   [2]string{"\x1B[31m", "\x1B[0m"},   // red
}

// shouldColor returns true when stdout is a TTY and NO_COLOR is not set.
func shouldColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// PrintJSON prints raw JSON bytes to stdout, pretty-formatted.
// Colorized when stdout is a TTY and NO_COLOR is not set. Key order is preserved.
func PrintJSON(data []byte) error {
	var err error
	if shouldColor() {
		_, err = fmt.Fprint(os.Stdout, string(pretty.Color(pretty.Pretty(data), darkStyle)))
	} else {
		_, err = fmt.Fprint(os.Stdout, string(pretty.Pretty(data)))
	}
	return err
}

// PrintJQValue prints a single jq result value with raw scalar output.
// Strings are unquoted. Numbers and bools are printed as-is.
// Null produces an empty line. Objects and arrays are JSON-encoded.
func PrintJQValue(v any, w io.Writer) error {
	switch val := v.(type) {
	case string:
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
		_, err := fmt.Fprint(w, buf.String())
		return err
	}
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}


package output

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
)

// WriteObject writes m as JSON indented by two spaces per level, byte for
// byte what json.Encoder with SetEscapeHTML(false) and SetIndent("", "  ")
// writes, but encodes array members one element at a time, so an encoded
// copy of the whole object is never held in memory. size, if not nil, gets
// the encoded length of each member's value. WriteObject returns the number
// of bytes written.
func WriteObject(w io.Writer, m map[string]any, size func(key string, n int)) (int64, error) {
	cw := &countingWriter{w: w}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encode writes v as a value that starts at the given nesting depth.
	encode := func(v any, depth int) error {
		buf.Reset()
		enc.SetIndent(indent(depth), "  ")
		if err := enc.Encode(v); err != nil {
			return err
		}
		cw.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
		return nil
	}

	if len(m) == 0 {
		cw.WriteString("{}\n")
		return cw.n, cw.err
	}
	cw.WriteString("{")
	for i, k := range slices.Sorted(maps.Keys(m)) {
		if i > 0 {
			cw.WriteString(",")
		}
		cw.WriteString("\n" + indent(1))
		if err := encode(k, 1); err != nil {
			return cw.n, err
		}
		cw.WriteString(": ")
		start := cw.n
		if arr, ok := m[k].([]any); ok && len(arr) > 0 {
			cw.WriteString("[")
			for j, el := range arr {
				if j > 0 {
					cw.WriteString(",")
				}
				cw.WriteString("\n" + indent(2))
				if err := encode(el, 2); err != nil {
					return cw.n, err
				}
			}
			cw.WriteString("\n" + indent(1) + "]")
		} else if err := encode(m[k], 1); err != nil {
			return cw.n, err
		}
		if size != nil {
			size(k, int(cw.n-start))
		}
		if cw.err != nil {
			return cw.n, cw.err
		}
	}
	cw.WriteString("\n}\n")
	return cw.n, cw.err
}

func indent(depth int) string { return strings.Repeat("  ", depth) }

// countingWriter counts the bytes written to w and keeps the first error,
// after which it writes nothing.
type countingWriter struct {
	w   io.Writer
	n   int64
	err error
}

func (c *countingWriter) Write(p []byte) {
	if c.err != nil {
		return
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.err = err
}

func (c *countingWriter) WriteString(s string) { c.Write([]byte(s)) }

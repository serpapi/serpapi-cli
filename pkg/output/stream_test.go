package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// encoded is what WriteObject must match: json.Encoder indenting by two
// spaces, without HTML escaping.
func encoded(t testing.TB, m map[string]any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeObject(data []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	return m, dec.Decode(&m) == nil && m != nil
}

func TestWriteObjectMatchesTheEncoder(t *testing.T) {
	inputs := []string{
		`{}`,
		`{"organic_results":[{"position":1,"title":"<b>Tom & Jerry</b>","link":"https://e.com/?a=1&b=2"}],"search_metadata":{"id":"abc"}}`,
		`{"b":[],"a":[[1,2],[]],"c":null,"d":[null,true,false,1.50,-0,1e400],"é":"\u2028\u2029 \ud83d\ude00 \u0000"}`,
		`{"z":{"y":[{"x":[1]}]},"Z":"upper sorts first","":"empty key"}`,
	}
	for _, in := range inputs {
		m, ok := decodeObject([]byte(in))
		if !ok {
			t.Fatalf("bad input %s", in)
		}
		m["nil slice"] = []any(nil)
		m["invalid utf-8"] = "a\xffb"
		var got bytes.Buffer
		n, err := WriteObject(&got, m, nil)
		if want := encoded(t, m); err != nil || !bytes.Equal(got.Bytes(), want) || n != int64(len(want)) {
			t.Errorf("WriteObject(%s) = %d, %v\n%s\nwant\n%s", in, n, err, got.Bytes(), want)
		}
	}
}

func TestWriteObjectReportsEachValueSize(t *testing.T) {
	m, _ := decodeObject([]byte(`{"organic_results":[{"position":1},{"position":2}],"search_metadata":{"id":"abc"},"x":"y"}`))
	sizes := map[string]int{}
	if _, err := WriteObject(&bytes.Buffer{}, m, func(k string, n int) { sizes[k] = n }); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"organic_results": len("[\n    {\n      \"position\": 1\n    },\n    {\n      \"position\": 2\n    }\n  ]"),
		"search_metadata": len("{\n    \"id\": \"abc\"\n  }"),
		"x":               len(`"y"`),
	}
	for k, n := range want {
		if sizes[k] != n {
			t.Errorf("size of %s = %d, want %d", k, sizes[k], n)
		}
	}
}

type failAfter struct{ left int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.left -= len(p); f.left < 0 {
		return 0, errors.New("broken pipe")
	}
	return len(p), nil
}

func TestWriteObjectStopsAtTheFirstWriteError(t *testing.T) {
	m, _ := decodeObject([]byte(`{"a":[1,2,3],"b":[4,5,6]}`))
	_, err := WriteObject(&failAfter{left: 5}, m, nil)
	if err == nil || err.Error() != "broken pipe" {
		t.Errorf("err = %v, want the write error", err)
	}
}

func FuzzWriteObjectMatchesTheEncoder(f *testing.F) {
	f.Add([]byte(`{"a":[1,{"b":"<&>"}],"c":"\u2028"}`))
	f.Add([]byte(`{"organic_results":[],"x":[[[]]],"n":-1.5e-7}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, ok := decodeObject(data)
		if !ok {
			return
		}
		var got bytes.Buffer
		if _, err := WriteObject(&got, m, nil); err != nil {
			t.Fatal(err)
		}
		if want := encoded(t, m); !bytes.Equal(got.Bytes(), want) {
			t.Errorf("WriteObject(%q)\n%s\nwant\n%s", data, got.Bytes(), want)
		}
	})
}

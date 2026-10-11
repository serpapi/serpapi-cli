package cmd

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

func FuzzMaskAPIKey(f *testing.F) {
	f.Add("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	f.Add("short")
	f.Add(`quote"inside\backslash`)
	f.Add("ключ-ключ-ключ")
	f.Add("日本語のキーです日本語")
	f.Add("<<<<<<<<<<&&&&")
	f.Fuzz(func(t *testing.T, key string) {
		quoted, _ := json.Marshal(key)
		doc := `{"account_email":"dev@example.com","api_key":` + string(quoted) + `,"total_searches_left":1}`
		out := maskAPIKey(json.RawMessage(doc))
		if !json.Valid(out) || utf8.ValidString(key) && !utf8.Valid(out) {
			t.Fatalf("masking %q made invalid JSON or UTF-8: %s", key, out)
		}
		var got struct {
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		if r := []rune(key); len(r) >= 9 && got.APIKey != string(r[:4])+"…"+string(r[len(r)-4:]) {
			t.Fatalf("key %q masked as %q", key, got.APIKey)
		}
	})
}

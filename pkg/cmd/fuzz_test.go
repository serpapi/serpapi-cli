package cmd

import (
	"encoding/json"
	"net/url"
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

func FuzzParseNextParams(f *testing.F) {
	f.Add("https://serpapi.com/search.json?engine=google&q=coffee&start=10", "organic_results")
	f.Add("http://[::1", "")
	f.Add("/search.json?q=%zz&q=a&json_restrictor=x", "error")
	f.Fuzz(func(t *testing.T, next, restrictor string) {
		carried := map[string]string{}
		if restrictor != "" {
			carried["json_restrictor"] = restrictor
		}
		got, err := parseNextParams(next, carried)
		if err != nil {
			return
		}
		if restrictor != "" && got["json_restrictor"] != restrictor {
			t.Fatalf("carried json_restrictor %q lost: %v", restrictor, got)
		}
		back, err := url.ParseQuery(canonicalParamsKey(got))
		if err != nil || len(back) != len(got) {
			t.Fatalf("canonical key %q does not round-trip %v", canonicalParamsKey(got), got)
		}
		for k, v := range got {
			if back.Get(k) != v {
				t.Fatalf("canonical key %q changed %s=%q to %q", canonicalParamsKey(got), k, v, back.Get(k))
			}
		}
	})
}

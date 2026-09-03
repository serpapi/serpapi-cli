package cmd

import (
	"testing"
)

func TestCanonicalParamsKeyIsOrderIndependent(t *testing.T) {
	a := map[string]string{
		"q":       "test",
		"start":   "0",
		"api_key": "abc",
	}
	b := map[string]string{
		"start":   "0",
		"api_key": "abc",
		"q":       "test",
	}
	if canonicalParamsKey(a) != canonicalParamsKey(b) {
		t.Errorf("canonical keys differ:\n  a=%q\n  b=%q", canonicalParamsKey(a), canonicalParamsKey(b))
	}
}

func TestParseNextParams(t *testing.T) {
	params, err := parseNextParams("https://serpapi.com/search.json?q=coffee&start=10&engine=google")
	if err != nil {
		t.Fatal(err)
	}
	if params["q"] != "coffee" {
		t.Errorf("expected q=coffee, got %s", params["q"])
	}
	if params["start"] != "10" {
		t.Errorf("expected start=10, got %s", params["start"])
	}
	if params["engine"] != "google" {
		t.Errorf("expected engine=google, got %s", params["engine"])
	}
}

func TestIsRawOutput(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]string
		want   bool
	}{
		{"no output param", map[string]string{"q": "coffee"}, false},
		{"output json", map[string]string{"output": "json"}, false},
		{"output JSON uppercase", map[string]string{"output": "JSON"}, false},
		{"output empty", map[string]string{"output": ""}, false},
		{"output md", map[string]string{"output": "md"}, true},
		{"output html", map[string]string{"output": "html"}, true},
		{"output md with whitespace", map[string]string{"output": " md "}, true},
	}
	for _, tc := range cases {
		if got := isRawOutput(tc.params); got != tc.want {
			t.Errorf("%s: isRawOutput = %v, want %v", tc.name, got, tc.want)
		}
	}
}

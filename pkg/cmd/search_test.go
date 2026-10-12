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
	const restrictor = "organic_results[].{title,link},serpapi_pagination.next"
	params, err := parseNextParams(
		"https://serpapi.com/search.json?q=coffee&start=10&engine=google",
		map[string]string{"json_restrictor": restrictor},
	)
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
	if params["json_restrictor"] != restrictor {
		t.Errorf("expected json_restrictor=%q, got %q", restrictor, params["json_restrictor"])
	}
}

func TestParseNextParamsCarriedParamsWin(t *testing.T) {
	next := "https://serpapi.com/search.json?engine=google&q=coffee&start=10&output=html"
	params, err := parseNextParams(next, map[string]string{"output": "json", "no_cache": "true"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"engine": "google", "q": "coffee", "start": "10", "output": "json", "no_cache": "true"}
	if canonicalParamsKey(params) != canonicalParamsKey(want) {
		t.Errorf("got %v, want %v", params, want)
	}
}

func TestParseNextParamsRejectsURLWithoutQuery(t *testing.T) {
	for _, next := range []string{"", "https://serpapi.com/search.json", "%zz"} {
		if _, err := parseNextParams(next, nil); err == nil {
			t.Errorf("parseNextParams(%q) succeeded", next)
		}
	}
}

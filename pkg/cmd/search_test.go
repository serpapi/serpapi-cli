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

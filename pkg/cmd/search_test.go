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

func TestWithPaginationRestrictor(t *testing.T) {
	if got := withPaginationRestrictor(""); got != "" {
		t.Errorf("expected empty restrictor, got %q", got)
	}

	const fields = "organic_results[].{title,link}"
	want := fields + ",serpapi_pagination.next"
	if got := withPaginationRestrictor(fields); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseNextParams(t *testing.T) {
	const restrictor = "organic_results[].{title,link},serpapi_pagination.next"
	params, err := parseNextParams(
		"https://serpapi.com/search.json?q=coffee&start=10&engine=google",
		restrictor,
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

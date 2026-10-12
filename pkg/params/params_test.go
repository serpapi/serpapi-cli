package params

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

func TestParseParamKeyValue(t *testing.T) {
	p, err := ParseParam("engine=google")
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "engine" || p.Value != "google" {
		t.Errorf("expected engine=google, got %s=%s", p.Key, p.Value)
	}
}

func TestParseParamBareWord(t *testing.T) {
	p, err := ParseParam("coffee")
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "q" || p.Value != "coffee" {
		t.Errorf("expected q=coffee, got %s=%s", p.Key, p.Value)
	}
}

func TestParseParamAPIKeyForbidden(t *testing.T) {
	_, err := ParseParam("api_key=secret")
	if err == nil {
		t.Fatal("expected error for api_key parameter")
	}
	if _, ok := err.(*clierrors.UsageError); !ok {
		t.Errorf("expected UsageError, got %T", err)
	}
}

func TestParseParamEmptyKey(t *testing.T) {
	_, err := ParseParam("=value")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if _, ok := err.(*clierrors.UsageError); !ok {
		t.Errorf("expected UsageError, got %T", err)
	}
}

func TestParseParamEmpty(t *testing.T) {
	_, err := ParseParam("")
	if err == nil {
		t.Fatal("expected error for empty parameter")
	}
	if _, ok := err.(*clierrors.UsageError); !ok {
		t.Errorf("expected UsageError, got %T", err)
	}
}

func TestParseParamRejectsAPIKeyAliases(t *testing.T) {
	for _, key := range []string{"api_key", "apikey", "key", "serp_api_key", "serpapi_key"} {
		_, err := ParseParam(key + "=secret")
		var usageErr *clierrors.UsageError
		if !errors.As(err, &usageErr) || strings.Contains(usageErr.Message, "secret") {
			t.Errorf("%s: got %v, want UsageError without the value", key, err)
		}
	}
}

func TestParseAllJoinsBareWordsIntoQuery(t *testing.T) {
	got, err := ParseAll([]string{"engine=google", "coffee", "shops", "location=Austin", "open"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Param{{"engine", "google"}, {"q", "coffee shops open"}, {"location", "Austin"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseAllRejectsBareWordsWithExplicitQuery(t *testing.T) {
	_, err := ParseAll([]string{"q=coffee", "shops"})
	var usageErr *clierrors.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("got %v, want UsageError", err)
	}
}

func TestRepeated(t *testing.T) {
	tests := []struct {
		args []string
		want []Param
	}{
		{[]string{"engine=google", "q=a"}, []Param{}},
		{[]string{"engine=google", "engine=google"}, []Param{}},
		{[]string{"q=a", "gl=us", "q=b", "gl=uk", "q=c"}, []Param{{"q", "c"}, {"gl", "uk"}}},
		{[]string{"tbs=x", "tbs=y", "tbs=x"}, []Param{{"tbs", "x"}}},
		{[]string{"location=", "location=Austin"}, []Param{{"location", "Austin"}}},
	}
	for _, tt := range tests {
		parsed, err := ParseAll(tt.args)
		if err != nil {
			t.Fatal(err)
		}
		if got := Repeated(parsed); fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("Repeated(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestApplyFields(t *testing.T) {
	tests := []struct {
		name, fields, param string
		extra               []string
		want                string
	}{
		{name: "none"},
		{name: "flag", fields: "organic_results[].{title, link}", extra: []string{"error"},
			want: "organic_results[].{title, link},error"},
		{name: "param", param: "organic_results", extra: []string{"error"}, want: "organic_results,error"},
		{name: "flag overrides param", fields: "a", param: "b", want: "a"},
		{name: "empty segments dropped", fields: " a,, b ,", extra: []string{"error"}, want: "a, b ,error"},
		{name: "only commas", fields: ", ,", extra: []string{"error"}},
		{name: "extra already present", fields: "a, error", extra: []string{"error", "serpapi_pagination.next"},
			want: "a, error,serpapi_pagination.next"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := map[string]string{}
			if tt.param != "" {
				p["json_restrictor"] = tt.param
			}
			got := ApplyFields(p, tt.fields, tt.extra...)
			if got != tt.want || p["json_restrictor"] != tt.want {
				t.Errorf("got %q (param %q), want %q", got, p["json_restrictor"], tt.want)
			}
			if _, ok := p["json_restrictor"]; ok != (tt.want != "") {
				t.Errorf("json_restrictor presence = %v", ok)
			}
		})
	}
}

func FuzzParseAll(f *testing.F) {
	f.Add("engine=google", "coffee", "shops")
	f.Add("q=a=b", "api_key=x", "")
	f.Add("=x", "key=", "café ☕")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		args := []string{a, b, c}
		parsed, err := ParseAll(args)
		if err != nil {
			return
		}
		for _, p := range parsed {
			if keyParams[p.Key] {
				t.Fatalf("ParseAll(%q) kept key parameter %q", args, p.Key)
			}
		}
		if !strings.Contains(a+b+c, "=") && (len(parsed) != 1 || parsed[0] != (Param{"q", strings.Join(args, " ")})) {
			t.Fatalf("ParseAll(%q) = %v, want one q of the joined words", args, parsed)
		}
	})
}

func FuzzApplyFields(f *testing.F) {
	f.Add("organic_results[0:3]", "", "error")
	f.Add(" , ,", "search_metadata", "serpapi_pagination.next")
	f.Add("", "a,,b ,", "a")
	f.Fuzz(func(t *testing.T, fields, existing, extra string) {
		if strings.Contains(extra, ",") || strings.TrimSpace(extra) != extra || extra == "" {
			return
		}
		p := map[string]string{}
		if existing != "" {
			p["json_restrictor"] = existing
		}
		got := ApplyFields(p, fields, extra)
		if got == "" {
			if _, ok := p["json_restrictor"]; ok {
				t.Fatalf("no restrictor in effect, but json_restrictor=%q is still sent", p["json_restrictor"])
			}
			return
		}
		if p["json_restrictor"] != got {
			t.Fatalf("returned %q but sends %q", got, p["json_restrictor"])
		}
		count := 0
		for _, s := range strings.Split(got, ",") {
			if strings.TrimSpace(s) == "" {
				t.Fatalf("restrictor %q has an empty segment", got)
			}
			if strings.TrimSpace(s) == extra {
				count++
			}
		}
		if count == 0 {
			t.Fatalf("restrictor %q lost %q", got, extra)
		}
		again := map[string]string{"json_restrictor": got}
		if twice := ApplyFields(again, "", extra); twice != got {
			t.Fatalf("not idempotent: %q then %q", got, twice)
		}
	})
}

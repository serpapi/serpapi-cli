package params

import (
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

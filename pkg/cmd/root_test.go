package cmd

import (
	"errors"
	"testing"
	"time"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

func TestDebugEnabled(t *testing.T) {
	tests := []struct {
		name string
		flag bool
		env  string
		want bool
	}{
		{name: "default off"},
		{name: "flag on", flag: true, want: true},
		{name: "env 1", env: "1", want: true},
		{name: "env true", env: "true", want: true},
		{name: "env 0", env: "0"},
		{name: "env false", env: "FALSE"},
		{name: "env off", env: "off"},
		{name: "flag wins over env 0", flag: true, env: "0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			debugFlag = tt.flag
			t.Cleanup(func() { debugFlag = false })
			t.Setenv("SERPAPI_DEBUG", tt.env)

			if got := debugEnabled(); got != tt.want {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: api.DefaultTimeout},
		{name: "flag seconds", flag: "120", want: 120 * time.Second},
		{name: "flag fractional", flag: "1.5", want: 1500 * time.Millisecond},
		{name: "flag zero disables", flag: "0", want: 0},
		{name: "env fallback", env: "90", want: 90 * time.Second},
		{name: "flag overrides env", flag: "10", env: "90", want: 10 * time.Second},
		{name: "negative rejected", flag: "-5", wantErr: true},
		{name: "garbage rejected", flag: "soon", wantErr: true},
		{name: "env garbage rejected", env: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeoutFlag = tt.flag
			t.Cleanup(func() { timeoutFlag = "" })
			t.Setenv("SERPAPI_TIMEOUT", tt.env)

			got, err := resolveTimeout()
			if tt.wantErr {
				var ue *clierrors.UsageError
				if !errors.As(err, &ue) {
					t.Fatalf("expected UsageError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

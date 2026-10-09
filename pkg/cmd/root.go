package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/jq"
	"github.com/serpapi/serpapi-cli/pkg/output"
	"github.com/serpapi/serpapi-cli/pkg/spinner"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

var (
	apiKeyFlag  string
	fieldsFlag  string
	jqFlag      string
	timeoutFlag string
	debugFlag   bool
)

var rootCmd = &cobra.Command{
	Use:           "serpapi",
	Short:         "HTTP client for structured web search data via SerpApi",
	Example:       `  serpapi search engine=google q=coffee
  serpapi search engine=google_light q="best pizza NYC" --jq ".organic_results[:3]"
  serpapi account`,
	Version:       version.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		_ = cmd.Help()
		return &clierrors.UsageError{Message: "No command specified. See usage above."}
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		clierrors.PrintError(err)
		os.Exit(clierrors.ExitCode(err))
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiKeyFlag, "api-key", "", "SerpApi API key (env: SERPAPI_KEY)")
	rootCmd.PersistentFlags().StringVar(&fieldsFlag, "fields", "", "Restrict JSON fields (server-side json_restrictor)")
	rootCmd.PersistentFlags().StringVar(&jqFlag, "jq", "", "Apply jq filter to output")
	rootCmd.PersistentFlags().StringVar(&timeoutFlag, "timeout", "",
		fmt.Sprintf("HTTP request timeout in seconds, 0 to disable (default %d; env: SERPAPI_TIMEOUT)", int(api.DefaultTimeout/time.Second)))
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false,
		"Print request timing and connection details (DNS, TLS, first byte, headers) to stderr (env: SERPAPI_DEBUG=1)")

	// Wrap cobra flag-parsing errors as UsageError so they get exit code 2.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &clierrors.UsageError{Message: err.Error()}
	})
}

// resolveAPIKey merges --api-key flag with SERPAPI_KEY env var, then config file.
func resolveAPIKey() (string, error) {
	key := apiKeyFlag
	if key == "" {
		key = os.Getenv("SERPAPI_KEY")
	}
	return config.ResolveAPIKey(key)
}

// resolveAPIKeyOptional resolves the API key but returns empty string instead of error.
func resolveAPIKeyOptional() string {
	key := apiKeyFlag
	if key == "" {
		key = os.Getenv("SERPAPI_KEY")
	}
	if key != "" {
		return key
	}
	if k, ok := config.LoadAPIKey(); ok {
		return k
	}
	return ""
}

// resolveTimeout merges --timeout with SERPAPI_TIMEOUT, falling back to
// api.DefaultTimeout. Values are whole or fractional seconds; 0 disables.
func resolveTimeout() (time.Duration, error) {
	raw := timeoutFlag
	source := "--timeout"
	if raw == "" {
		raw = os.Getenv("SERPAPI_TIMEOUT")
		source = "SERPAPI_TIMEOUT"
	}
	if raw == "" {
		return api.DefaultTimeout, nil
	}
	return parseTimeout(raw, source)
}

func parseTimeout(raw, source string) (time.Duration, error) {
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs < 0 || secs != secs { // secs != secs rejects NaN
		return 0, &clierrors.UsageError{
			Message: fmt.Sprintf("Invalid %s value %q: expected a non-negative number of seconds", source, raw),
		}
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// debugEnabled reports whether --debug or SERPAPI_DEBUG is set.
func debugEnabled() bool {
	if debugFlag {
		return true
	}
	switch strings.ToLower(os.Getenv("SERPAPI_DEBUG")) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// newClient builds an API client honoring the configured request timeout
// and debug tracing.
func newClient(apiKey string) (*api.Client, error) {
	timeout, err := resolveTimeout()
	if err != nil {
		return nil, err
	}
	client := api.NewWithTimeout(apiKey, timeout)
	if debugEnabled() {
		client.SetDebugWriter(os.Stderr)
	}
	return client, nil
}

// newSpinner creates a spinner with the given label. The spinner is disabled
// in debug mode so its redraws don't interleave with trace output.
func newSpinner(label string) *spinner.Spinner {
	if debugEnabled() {
		return &spinner.Spinner{}
	}
	return spinner.New(label)
}

// handleOutput applies --jq filtering and prints result.
func handleOutput(raw json.RawMessage) error {
	if jqFlag != "" {
		// Unmarshal for jq processing; use decoder with UseNumber to preserve integer fidelity.
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return &clierrors.APIError{Message: "Failed to parse response: " + err.Error()}
		}
		results, err := jq.Apply(jqFlag, v)
		if err != nil {
			return &clierrors.UsageError{Message: err.Error()}
		}
		for _, val := range results {
			if err := output.PrintJQValue(val, os.Stdout); err != nil {
				return &clierrors.APIError{Message: fmt.Sprintf("Output error: %s", err)}
			}
		}
		return nil
	}
	if err := output.PrintJSON(raw); err != nil {
		return &clierrors.APIError{Message: fmt.Sprintf("Output error: %s", err)}
	}
	return nil
}

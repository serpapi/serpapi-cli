package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/jq"
	"github.com/serpapi/serpapi-cli/pkg/output"
	"github.com/serpapi/serpapi-cli/pkg/spinner"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

var (
	apiKeyFlag string
	fieldsFlag string
	jqFlag     string
)

var rootCmd = &cobra.Command{
	Use:           "serpapi",
	Short:         "HTTP client for structured web search data via SerpApi",
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

// newSpinner creates a spinner with the given label.
func newSpinner(label string) *spinner.Spinner {
	return spinner.New(label)
}

package cmd

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/params"
)

var locationsCmd = &cobra.Command{
	Use:   "locations q=QUERY [limit=N]",
	Short: "Find values for the location parameter (free, no API key)",
	Long: `Find supported locations. Pass a result's canonical_name as location= in a
search. limit is at most 10. This request is free and needs no API key.`,
	Example: `  serpapi locations q=Austin limit=3
  serpapi locations q=Austin --jq ".[0].canonical_name"`,
	Args: cobra.ArbitraryArgs,
	RunE: runLocations,
}

func init() {
	rootCmd.AddCommand(locationsCmd)
}

func runLocations(cmd *cobra.Command, args []string) error {
	parsed, err := params.ParseAll(args)
	if err != nil {
		return err
	}
	p := params.ParamsToMap(parsed)
	if blank(p["q"]) && blank(p["location"]) && blank(p["limit"]) {
		return &clierrors.UsageError{Message: "Pass a location query, e.g. serpapi locations q=Austin limit=5; without one the server returns every location (66 MB)"}
	}
	client := newClient("")
	result, err := withSpinner("Fetching locations...", func() (json.RawMessage, error) {
		return client.Locations(cmd.Context(), p)
	})
	if err != nil {
		return err
	}
	return handleOutput(cmd, result)
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

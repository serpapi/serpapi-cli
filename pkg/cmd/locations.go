package cmd

import (
	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/params"
)

var locationsCmd = &cobra.Command{
	Use:   "locations [PARAMS...]",
	Short: "Lookup available locations for search queries",
	Args:  cobra.ArbitraryArgs,
	RunE:  runLocations,
}

func init() {
	rootCmd.AddCommand(locationsCmd)
}

func runLocations(cmd *cobra.Command, args []string) error {
	parsed, err := params.ParseAll(args)
	if err != nil {
		return err
	}

	paramsMap := params.ParamsToMap(parsed)
	client, err := newClient("")
	if err != nil {
		return err
	}

	sp := newSpinner("Fetching locations...")
	sp.Start()
	defer sp.Stop()
	result, err := client.Locations(cmd.Context(), paramsMap)
	if err != nil {
		return err
	}

	return handleOutput(result)
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version, as --version does",
	Args:  noArgs("run serpapi version"),
	RunE: func(cmd *cobra.Command, _ []string) error {
		if jqFlag != "" {
			return &clierrors.UsageError{Message: "version prints text, not JSON; drop --jq"}
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "serpapi version %s\n", version.Version)
		return outputError(err)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

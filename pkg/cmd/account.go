package cmd

import (
	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Retrieve account information and usage statistics",
	Args:  cobra.NoArgs,
	RunE:  runAccount,
}

func init() {
	rootCmd.AddCommand(accountCmd)
}

func runAccount(cmd *cobra.Command, args []string) error {
	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}

	sp := newSpinner("Fetching account...")
	sp.Start()
	defer sp.Stop()
	client := api.New(apiKey)
	result, err := client.Account(cmd.Context())
	if err != nil {
		return err
	}

	return handleOutput(result)
}

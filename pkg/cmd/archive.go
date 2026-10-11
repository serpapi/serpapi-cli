package cmd

import (
	"encoding/json"
	"regexp"

	"github.com/spf13/cobra"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

var reArchiveID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var archiveCmd = &cobra.Command{
	Use:   "archive ID",
	Short: "Fetch an earlier search by its search_metadata.id (free)",
	Long: `Fetch an earlier search by the ID in its search_metadata.id. This request is
free, and it is how you collect the results of a search made with async=true:
until search_metadata.status is Success it is Queued or Processing, so poll.`,
	Example: `  serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f
  serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f --jq ".organic_results[0]"`,
	Args: cobra.ExactArgs(1),
	RunE: runArchive,
}

func init() {
	rootCmd.AddCommand(archiveCmd)
}

func runArchive(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !reArchiveID.MatchString(id) {
		return &clierrors.UsageError{Message: "Invalid archive ID: must contain only alphanumeric characters, dashes, and underscores"}
	}

	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}
	client := newClient(apiKey)
	result, err := withSpinner("Fetching archive...", func() (json.RawMessage, error) { return client.Archive(cmd.Context(), id) })
	if err != nil {
		return err
	}
	return handleOutput(cmd, result)
}

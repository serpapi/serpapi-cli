package cmd

import (
	"regexp"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

var reArchiveID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var archiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Retrieve a previously cached search by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runArchive,
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

	sp := newSpinner("Fetching archive...")
	sp.Start()
	defer sp.Stop()
	client := api.New(apiKey)
	result, err := client.Archive(cmd.Context(), id)
	if err != nil {
		return err
	}

	return handleOutput(result)
}

package cmd

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/output"
)

var reArchiveID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var archiveCmd = &cobra.Command{
	Use:   "archive ID [output=FORMAT]",
	Short: "Fetch an earlier search by its search_metadata.id (free)",
	Long: `Fetch an earlier search by the ID in its search_metadata.id. This request is
free, and it is how you collect the results of a search made with async=true:
until search_metadata.status is Success it is Queued or Processing, so poll.
output= takes the same values as in search, such as md for Markdown.`,
	Example: `  serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f
  serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f output=md
  serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f --jq ".organic_results[0]"`,
	Args: cobra.RangeArgs(1, 2),
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
	format := "json"
	if len(args) == 2 {
		v, ok := strings.CutPrefix(args[1], "output=")
		if !ok {
			return &clierrors.UsageError{Message: "Unexpected argument " + args[1] + "; archive takes an ID and optionally output=FORMAT"}
		}
		format = v
	}
	raw, err := isRawOutput(format)
	if err != nil {
		return err
	}
	if raw && jqFlag != "" {
		return &clierrors.UsageError{Message: "--jq needs JSON, but output=" + format + " is not JSON"}
	}

	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}
	client := newClient(apiKey)
	ctx := cmd.Context()

	if format == "json" {
		result, err := withSpinner("Fetching archive...", func() (json.RawMessage, error) { return client.Archive(ctx, id) })
		if err != nil {
			return err
		}
		return handleOutput(cmd, result, archiveHint)
	}
	body, err := withSpinner("Fetching archive...", func() ([]byte, error) { return client.ArchiveRaw(ctx, id, format) })
	if err != nil {
		return err
	}
	if raw {
		return outputError(output.PrintRaw(cmd.OutOrStdout(), body))
	}
	return handleOutput(cmd, body, archiveHint)
}

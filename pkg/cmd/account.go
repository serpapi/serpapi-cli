package cmd

import (
	"encoding/json"
	"regexp"

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

	return handleOutput(maskAPIKey(result))
}

var reAPIKey = regexp.MustCompile(`("api_key"\s*:\s*")([^"]{9,})(")`)

// maskAPIKey replaces the api_key value in raw JSON with a masked version (first 4 + last 4).
// Operates on raw bytes to preserve original field order.
func maskAPIKey(raw json.RawMessage) json.RawMessage {
	return reAPIKey.ReplaceAllFunc(raw, func(match []byte) []byte {
		sub := reAPIKey.FindSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		key := string(sub[2])
		masked := key[:4] + "…" + key[len(key)-4:]
		var buf []byte
		buf = append(buf, sub[1]...)
		buf = append(buf, masked...)
		buf = append(buf, sub[3]...)
		return buf
	})
}

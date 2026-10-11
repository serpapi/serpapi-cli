package cmd

import (
	"encoding/json"
	"regexp"

	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Show plan, usage and searches left (free)",
	Long: `Show the account's plan, usage this month and hour, and searches left.
This request is free. The API key in the output is masked.`,
	Example: `  serpapi account --jq .total_searches_left
  serpapi account --jq "{plan_name, total_searches_left, account_rate_limit_per_hour}"`,
	Args: noArgs("pass the key with SERPAPI_KEY or --api-key"),
	RunE: runAccount,
}

func init() {
	rootCmd.AddCommand(accountCmd)
}

func runAccount(cmd *cobra.Command, _ []string) error {
	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}
	client := newClient(apiKey)
	result, err := withSpinner("Fetching account...", func() (json.RawMessage, error) {
		return client.Account(cmd.Context())
	})
	if err != nil {
		return err
	}
	return handleOutput(cmd, maskAPIKey(result), nil)
}

var reAPIKey = regexp.MustCompile(`("api_key"\s*:\s*)("(?:[^"\\]|\\.)*")`)

// maskAPIKey replaces the api_key value in raw JSON with its first and last
// four characters. It edits the bytes in place to keep the field order, and
// decodes the value so escapes and multi-byte characters are never cut.
func maskAPIKey(raw json.RawMessage) json.RawMessage {
	return reAPIKey.ReplaceAllFunc(raw, func(match []byte) []byte {
		sub := reAPIKey.FindSubmatch(match)
		var key string
		if json.Unmarshal(sub[2], &key) != nil {
			return match
		}
		r := []rune(key)
		if len(r) < 9 {
			return match
		}
		masked, err := json.Marshal(string(r[:4]) + "…" + string(r[len(r)-4:]))
		if err != nil {
			return match
		}
		return append(append([]byte{}, sub[1]...), masked...)
	})
}

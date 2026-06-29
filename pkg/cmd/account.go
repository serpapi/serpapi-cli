package cmd

import (
	"encoding/json"

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

// maskAPIKey replaces the api_key value with a masked version showing only the last 4 chars.
func maskAPIKey(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if keyVal, ok := m["api_key"]; ok {
		var key string
		if err := json.Unmarshal(keyVal, &key); err == nil && len(key) > 8 {
			masked := key[:4] + "…" + key[len(key)-4:]
			b, _ := json.Marshal(masked)
			m["api_key"] = b
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

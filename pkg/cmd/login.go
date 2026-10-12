package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/output"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Check an API key and save it for later commands",
	Long: `Check an API key with a free account request, then save it to the config
file. The key comes from --api-key, a hidden prompt on a terminal, or else the
first line of stdin, which login waits for. In scripts and agents, set
SERPAPI_KEY instead; it takes precedence over the saved key. Get a key at
https://serpapi.com/manage-api-key`,
	Example: `  serpapi login
  printf %s "$KEY" | serpapi login`,
	Args: noArgs(`pipe the key instead: printf %s "$KEY" | serpapi login`),
	RunE: runLogin,
}

func init() {
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, _ []string) error {
	apiKey := strings.TrimSpace(apiKeyFlag)
	if apiKey == "" {
		key, err := readAPIKey(cmd)
		if err != nil || key == "" {
			return err
		}
		apiKey = key
	}

	email, err := accountEmail(cmd.Context(), apiKey)
	if err != nil {
		return err
	}
	if err := config.SaveConfig(apiKey); err != nil {
		return err
	}
	// Without a terminal, success is exit 0 and stderr carries only JSON lines.
	if f, ok := cmd.ErrOrStderr().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(f, "Logged in as %s. API key saved to %s\n", email, config.Path())
	}
	for _, name := range apiKeyEnvVars {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			warn(cmd, "%s is set and takes precedence over the saved key", name)
			break
		}
	}
	return nil
}

// readAPIKey prompts on a terminal and otherwise reads the first line of
// stdin. It returns "" and no error when the user keeps the saved key.
func readAPIKey(cmd *cobra.Command) (string, error) {
	stderr := cmd.ErrOrStderr()
	var key string
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		if saved, err := config.LoadAPIKey(); err == nil && saved != "" {
			if email, err := accountEmail(cmd.Context(), saved); err == nil {
				fmt.Fprintf(stderr, "Already logged in as %s. Replace the saved key? [y/N] ", email)
				line, _ := bufio.NewReader(f).ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") {
					fmt.Fprintln(stderr, "Kept the saved key.")
					return "", nil
				}
			}
		}
		fmt.Fprint(stderr, "SerpApi API key (input hidden): ")
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", &clierrors.UsageError{Message: "Failed to read the API key: " + err.Error()}
		}
		key = string(b)
	} else {
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", &clierrors.UsageError{Message: "Failed to read the API key: " + err.Error()}
		}
		key = line
	}
	if key = strings.TrimSpace(key); key == "" {
		return "", &clierrors.UsageError{Message: `No API key given. Use: printf %s "$KEY" | serpapi login`}
	}
	return key, nil
}

func accountEmail(ctx context.Context, apiKey string) (string, error) {
	raw, err := newClient(apiKey).Account(ctx)
	if err != nil {
		return "", err
	}
	var account struct {
		AccountEmail string `json:"account_email"`
	}
	if json.Unmarshal(raw, &account) != nil || account.AccountEmail == "" {
		return "unknown", nil
	}
	return string(output.ForTerminal([]byte(account.AccountEmail))), nil
}

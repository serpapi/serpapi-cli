package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/serpapi/serpapi-cli/pkg/api"
	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Save API key to config file",
	Args:  cobra.NoArgs,
	RunE:  runLogin,
}

func init() {
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	var apiKey string

	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Enter your SerpApi API key: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr) // newline after hidden input
		if err != nil {
			return &clierrors.UsageError{Message: "Failed to read input: " + err.Error()}
		}
		apiKey = string(raw)
	} else {
		fmt.Fprint(os.Stderr, "Enter your SerpApi API key: ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return &clierrors.UsageError{Message: "Failed to read input: " + err.Error()}
		}
		apiKey = line
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return &clierrors.UsageError{Message: "API key cannot be empty."}
	}

	client := api.New(apiKey)
	raw, err := client.Account(cmd.Context())
	if err != nil {
		return err
	}

	email := "unknown"
	var account struct {
		AccountEmail string `json:"account_email"`
	}
	if json.Unmarshal(raw, &account) == nil && account.AccountEmail != "" {
		email = account.AccountEmail
	}

	if err := config.SaveConfig(apiKey); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Logged in as %s. API key saved to %q\n", email, config.Path())
	return nil
}


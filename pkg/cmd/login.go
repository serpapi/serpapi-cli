package cmd

import (
	"bufio"
	"bytes"
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
// stdin. It returns "" and no error when the user keeps the saved key. Ctrl-C
// or SIGTERM stops it at once with an interrupted error.
func readAPIKey(cmd *cobra.Command) (string, error) {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()
	var key string
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		if saved, err := config.LoadAPIKey(); err == nil && saved != "" {
			if email, err := accountEmail(ctx, saved); err == nil {
				fmt.Fprintf(stderr, "Already logged in as %s. Replace the saved key? [y/N] ", email)
				line, err := readInterruptibly(ctx, func() (string, error) { return bufio.NewReader(f).ReadString('\n') },
					"Stopped at the prompt; the saved key is unchanged")
				if isInterrupted(err) {
					fmt.Fprintln(stderr)
					return "", err
				}
				if !strings.EqualFold(strings.TrimSpace(line), "y") {
					fmt.Fprintln(stderr, "Kept the saved key.")
					return "", nil
				}
			}
		}
		const stopped = "Stopped at the API key prompt; nothing was saved"
		if ctx.Err() != nil {
			return "", &clierrors.InterruptedError{Message: stopped}
		}
		fmt.Fprint(stderr, "SerpApi API key (input hidden): ")
		line, err := readHidden(ctx, f, stderr, stopped)
		if err != nil {
			return "", err
		}
		key = line
	} else {
		in := bufio.NewReader(cmd.InOrStdin())
		line, err := readInterruptibly(ctx, func() (string, error) { return in.ReadString('\n') },
			"Stopped while waiting for the API key on stdin; nothing was saved")
		if isInterrupted(err) {
			return "", err
		}
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

// readHidden reads a line from the terminal f without showing it. Raw mode is
// set and undone here, not in the goroutine that reads, so a read that a
// signal leaves running can't turn echo off again after it was restored. In
// raw mode Ctrl-C is a key rather than a signal; it stops the run as the
// signal would, even after Enter in the same read. Ctrl-D gives no key.
func readHidden(ctx context.Context, f *os.File, stderr io.Writer, stopped string) (string, error) {
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", &clierrors.UsageError{Message: "Failed to read the API key: " + err.Error()}
	}
	in := &ctrlCWatch{r: f}
	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{in, stderr}, "")
	line, err := readInterruptibly(ctx, func() (string, error) { return t.ReadPassword("") }, stopped)
	_ = term.Restore(fd, state)
	if !isInterrupted(err) && in.ctrlC {
		stopRun(ctx)
		err = &clierrors.InterruptedError{Message: stopped}
	}
	if errors.Is(err, term.ErrPasteIndicator) {
		err = nil
	}
	if err != nil {
		fmt.Fprintln(stderr)
	}
	switch {
	case err == nil:
		return line, nil
	case isInterrupted(err):
		return "", err
	case errors.Is(err, io.EOF):
		return "", nil
	}
	return "", &clierrors.UsageError{Message: "Failed to read the API key: " + err.Error()}
}

// ctrlCWatch notes a Ctrl-C, which raw mode passes on as a key. Only the
// reading goroutine writes ctrlC; it is read after that goroutine returned.
type ctrlCWatch struct {
	r     io.Reader
	ctrlC bool
}

func (w *ctrlCWatch) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if bytes.IndexByte(p[:n], 3) >= 0 {
		w.ctrlC = true
	}
	return n, err
}

// readInterruptibly returns what read returns, or an interrupted error with
// the stopped message as soon as ctx is canceled. A read from a terminal or a
// pipe can't be canceled itself; it ends with the process. read outlives an
// interrupted call, so it must not touch the command.
func readInterruptibly[T any](ctx context.Context, read func() (T, error), stopped string) (T, error) {
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := read()
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, &clierrors.InterruptedError{Message: stopped}
	}
}

func isInterrupted(err error) bool {
	var interrupted *clierrors.InterruptedError
	return errors.As(err, &interrupted)
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

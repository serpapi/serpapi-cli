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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/jq"
	"github.com/serpapi/serpapi-cli/pkg/output"
	"github.com/serpapi/serpapi-cli/pkg/spinner"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

var (
	apiKeyFlag  string
	fieldsFlag  string
	jqFlag      string
	timeoutFlag string

	// requestTimeout is --timeout, else SERPAPI_TIMEOUT, as validateGlobalFlags
	// resolved it for this run.
	requestTimeout = api.DefaultTimeout

	// clientOptions are applied to every client after the defaults; tests use
	// it to point the CLI at a local server.
	clientOptions []api.Option
)

// apiKeyEnvVars are read in order after --api-key. SERPAPI_API_KEY is the name
// LangChain and the SerpApi MCP server use.
var apiKeyEnvVars = []string{"SERPAPI_KEY", "SERPAPI_API_KEY"}

var rootCmd = &cobra.Command{
	Use:   "serpapi",
	Short: "Search engine results as JSON from SerpApi",
	Long: `Search engine results as JSON from SerpApi (https://serpapi.com).

Parameters are key=value pairs sent to the API as-is; words without "=" are
joined into q. Engines and their parameters: https://serpapi.com/search-engine-apis

API key, first match wins: --api-key, SERPAPI_KEY, SERPAPI_API_KEY, then the
key saved by 'serpapi login'. Without one, search still runs, but the API
answers only a few demo queries, such as q=coffee, and returns 401 for others.

Results go to stdout as JSON indented by two spaces, colored on a terminal.
Piped, it is indented too, so head, grep and file viewers show whole lines;
head cuts the JSON short, so take parts with --jq. A terminal gets control
characters from results escaped, so a page cannot drive it; piped output keeps
them. Keep results small:
  --fields "organic_results[].{title, link}"   server-side filter (search only)
  --jq ".organic_results[].link"                jq filter; strings print unquoted

Errors and warnings go to stderr as one JSON object per line:
  {"error":{"code":"api_error","message":"Invalid API key. ...","status":401}}
  {"warning":{"message":"..."}}
Exit 2, code usage_error: the command is wrong; fix it before retrying.
Exit 1, code api_error or network_error. status is the HTTP status of the
answer: 200 the search ran but reported a problem, such as no results; 400
invalid parameters; 401 invalid API key; 403 account not allowed; 429 rate
limited or out of searches; 5xx SerpApi failed. No status means no usable
answer: a network_error, such as a timeout, or an unreadable response.
Requests time out after 60s: --timeout or SERPAPI_TIMEOUT sets seconds, 0
waits indefinitely.
Exit 130, code interrupted: stopped by Ctrl-C or SIGTERM, after printing what
had arrived.

Cost: each successful search counts against your monthly searches, including
each page of --all-pages; cached, errored and failed ones don't, though errored
ones count toward the hourly limit (https://serpapi.com/pricing). An identical
search, --fields included, repeated within an hour comes from the cache.
no_cache=true forces a new, counted search; cached results are at most an
hour old, so add it only when that is too old. account, locations and archive
are free.`,
	Example: `  serpapi search engine=google q=coffee --fields "organic_results[0:3]"
  serpapi search engine=google coffee shops --jq ".organic_results[].link"
  serpapi locations q=Austin limit=3
  serpapi account --jq .total_searches_left`,
	Version:       version.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SetOut(cmd.ErrOrStderr())
		_ = cmd.Help()
		return &clierrors.UsageError{Message: "No command given; see 'serpapi --help'"}
	},
}

// interruptGrace is how long a command may take to wrap up after the first
// signal before the process exits anyway.
var interruptGrace = 3 * time.Second

// Execute runs the root command. The first Ctrl-C or SIGTERM cancels the
// command's context so it can print what it has; a second one kills it.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go exitAfterGrace(ctx, done, stop, rootCmd.ErrOrStderr(), os.Exit)
	code := run(ctx)
	close(done)
	stop()
	os.Exit(code)
}

// exitAfterGrace waits for the first signal, restores the default handling
// so that a second one kills the process, and exits 130 if the command is
// still running interruptGrace later, as it is when blocked on a full pipe.
func exitAfterGrace(ctx context.Context, done <-chan struct{}, stop func(), stderr io.Writer, exit func(int)) {
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	stop()
	select {
	case <-done:
	case <-time.After(interruptGrace):
		// stderr may be the full pipe, so the message gets a moment, not a promise.
		go clierrors.FprintError(stderr, &clierrors.InterruptedError{
			Message: fmt.Sprintf("Stopped %s after the signal; the output may be cut off", interruptGrace),
		})
		time.Sleep(100 * time.Millisecond)
		exit(130)
	}
}

// run executes the root command, reports any error on its stderr as JSON and
// returns the exit code. A command stopped by canceling ctx exits 130.
func run(ctx context.Context) int {
	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var interrupted *clierrors.InterruptedError
	if ctx.Err() != nil && !errors.As(err, &interrupted) {
		err = &clierrors.InterruptedError{Message: "Stopped before the response arrived"}
	}
	clierrors.FprintError(rootCmd.ErrOrStderr(), err)
	return clierrors.ExitCode(err)
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&apiKeyFlag, "api-key", "", "SerpApi API key; prefer SERPAPI_KEY, since flags show up in ps and shell history")
	pf.StringVar(&fieldsFlag, "fields", "", `search only: server-side json_restrictor, e.g. "organic_results[].{title, link}"`)
	pf.StringVar(&jqFlag, "jq", "", `jq filter applied to the JSON result, e.g. ".organic_results[].link"`)
	pf.StringVar(&timeoutFlag, "timeout", "", fmt.Sprintf(
		"HTTP request timeout in seconds, such as 120, or a duration, such as 2m; 0 waits indefinitely (default %d; env SERPAPI_TIMEOUT)",
		int(api.DefaultTimeout/time.Second)))

	rootCmd.PersistentPreRunE = validateGlobalFlags

	// Wrap cobra flag-parsing errors as UsageError so they get exit code 2.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &clierrors.UsageError{Message: err.Error()}
	})
}

// validateGlobalFlags rejects bad flags before any request, so a typo never
// costs a search.
func validateGlobalFlags(cmd *cobra.Command, _ []string) error {
	timeout, err := resolveTimeout()
	if err != nil {
		return err
	}
	requestTimeout = timeout
	if fieldsFlag != "" && cmd != searchCmd {
		return &clierrors.UsageError{Message: "--fields works only with search; use --jq to filter " + cmd.Name() + " output"}
	}
	if jqFlag != "" {
		if _, err := jq.Compile(jqFlag); err != nil {
			return &clierrors.UsageError{Message: err.Error()}
		}
	}
	return nil
}

// resolveTimeout returns --timeout, else SERPAPI_TIMEOUT, else
// api.DefaultTimeout. 0 disables the limit.
func resolveTimeout() (time.Duration, error) {
	raw, source := strings.TrimSpace(timeoutFlag), "--timeout"
	if raw == "" {
		raw, source = strings.TrimSpace(os.Getenv("SERPAPI_TIMEOUT")), "SERPAPI_TIMEOUT"
	}
	if raw == "" {
		return api.DefaultTimeout, nil
	}
	return parseTimeout(raw, source)
}

// parseTimeout reads seconds, such as 120 or 1.5, or a duration, such as 2m.
func parseTimeout(raw, source string) (time.Duration, error) {
	invalid := &clierrors.UsageError{Message: fmt.Sprintf(
		"Invalid %s value %q: expected seconds, such as 120, or a duration, such as 2m; 0 waits indefinitely", source, raw)}
	if secs, err := strconv.ParseFloat(raw, 64); err == nil {
		// Also rejects NaN and infinity; 1e9 s (31 years) keeps time.Duration from overflowing.
		if !(secs >= 0 && secs <= 1e9) {
			return 0, invalid
		}
		d := time.Duration(secs * float64(time.Second))
		if secs > 0 && d == 0 { // under a nanosecond, which would mean no limit
			return 0, invalid
		}
		return d, nil
	}
	d, err := time.ParseDuration(raw)
	// A nonzero duration under a nanosecond, such as 0.5ns, also parses as 0.
	if err != nil || d < 0 || (d == 0 && strings.ContainsAny(raw, "123456789")) {
		return 0, invalid
	}
	return d, nil
}

// resolveAPIKey returns the first non-empty key from --api-key, the
// environment, then the config file.
func resolveAPIKey() (string, error) {
	if key := strings.TrimSpace(apiKeyFlag); key != "" {
		return key, nil
	}
	for _, name := range apiKeyEnvVars {
		if key := strings.TrimSpace(os.Getenv(name)); key != "" {
			return key, nil
		}
	}
	return config.ResolveAPIKey("")
}

func newClient(apiKey string) *api.Client {
	opts := append([]api.Option{api.WithTimeout(requestTimeout)}, clientOptions...)
	return api.New(apiKey, opts...)
}

// withSpinner shows a spinner on an interactive stderr while fn runs and
// clears it before the caller prints anything.
func withSpinner[T any](label string, fn func() (T, error)) (T, error) {
	sp := spinner.New(label)
	sp.Start()
	defer sp.Stop()
	return fn()
}

func warn(cmd *cobra.Command, format string, a ...any) {
	clierrors.PrintWarning(cmd.ErrOrStderr(), fmt.Sprintf(format, a...))
}

// noArgs rejects arguments without echoing them, as cobra.NoArgs would: a
// stray argument to login or account is often the API key.
func noArgs(hint string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return &clierrors.UsageError{Message: cmd.Name() + " takes no arguments; " + hint}
		}
		return nil
	}
}

// handleOutput applies --jq, if set, and prints the JSON result to stdout.
func handleOutput(cmd *cobra.Command, raw []byte) error {
	if jqFlag == "" {
		return outputError(output.PrintJSON(cmd.OutOrStdout(), raw))
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return &clierrors.APIError{Message: "Failed to parse response: " + err.Error()}
	}
	return printJQ(cmd, v)
}

// printJQ runs --jq on v and prints each result as it is produced.
func printJQ(cmd *cobra.Command, v any) error {
	code, err := jq.Compile(jqFlag)
	if err != nil {
		return &clierrors.UsageError{Message: err.Error()}
	}
	w := cmd.OutOrStdout()
	tty := output.IsTerminal(w)
	bw := bufio.NewWriter(w)
	var writeErr error
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	err = jq.Run(ctx, code, v, func(val any) error {
		writeErr = output.PrintJQValue(val, bw, tty)
		return writeErr
	})
	if flushErr := bw.Flush(); writeErr == nil {
		writeErr = flushErr
	}
	if writeErr != nil {
		return outputError(writeErr)
	}
	if err != nil && ctx.Err() != nil {
		return &clierrors.InterruptedError{Message: "Stopped while running --jq; what it printed before the signal is on stdout"}
	}
	if err != nil {
		return &clierrors.UsageError{Message: err.Error()}
	}
	return nil
}

func outputError(err error) error {
	if err == nil {
		return nil
	}
	return &clierrors.APIError{Message: "Output error: " + err.Error()}
}

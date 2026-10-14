package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
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
	debugFlag   bool

	// requestTimeout is --timeout, else SERPAPI_TIMEOUT, as validateGlobalFlags
	// resolved it for this run.
	requestTimeout = api.DefaultTimeout

	// debugRequests is --debug, else SERPAPI_DEBUG, as validateGlobalFlags
	// resolved it for this run.
	debugRequests bool

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
  output=md                                     Markdown instead of JSON

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
waits indefinitely. --debug or SERPAPI_DEBUG=1 traces each request on stderr
(DNS, connect, TLS, first byte, headers, server timeline): plain text on a
terminal, otherwise one {"debug":{...}} JSON object per line.
Exit 130, code interrupted: Ctrl-C or SIGTERM stopped it before it finished,
after printing what had arrived.

Cost: each successful search counts against your monthly searches, including
each page of --all-pages; cached, errored and failed ones don't, though errored
ones count toward the hourly limit (https://serpapi.com/pricing). An identical
search, --fields included, repeated within an hour comes from the cache.
no_cache=true forces a new, counted search; cached results are at most an
hour old, so add it only when that is too old. zero_trace=true (Enterprise)
stores nothing, so it skips the cache too and leaves no search for archive to
read. account, locations and archive are free.`,
	Example: `  serpapi search engine=google q=coffee --fields "organic_results[0:3]"
  serpapi search engine=google coffee shops --jq ".organic_results[].link"
  serpapi search engine=google q=coffee output=md
  serpapi locations q=Austin limit=3
  serpapi account --jq .total_searches_left`,
	Version:       version.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Off a terminal, stderr carries only JSON lines; the error names --help.
		if output.IsTerminal(cmd.ErrOrStderr()) {
			cmd.SetOut(cmd.ErrOrStderr())
			_ = cmd.Help()
		}
		return &clierrors.UsageError{Message: "No command given; see 'serpapi --help'"}
	},
}

// interruptGrace is how long a command may take to wrap up after the first
// signal before the process exits anyway.
var interruptGrace = 3 * time.Second

// signaled is the cause Execute cancels the command's context with, so code
// that sees the cancellation knows when the signal came.
type signaled struct{ at time.Time }

func (*signaled) Error() string { return "stopped by Ctrl-C or SIGTERM" }

// Unwrap keeps errors.Is(err, context.Canceled) true for a request it
// stopped: net/http reports the context's cause, not context.Canceled.
func (*signaled) Unwrap() error { return context.Canceled }

// exitTime is when the process exits after the first signal: interruptGrace
// after it. A context canceled without a signal time counts from now.
func exitTime(ctx context.Context) time.Time {
	var s *signaled
	if errors.As(context.Cause(ctx), &s) {
		return s.at.Add(interruptGrace)
	}
	return time.Now().Add(interruptGrace)
}

// stopRunKey holds, in the command's context, the func that cancels it as a
// signal does. login calls it for a Ctrl-C that raw mode turns into a key.
type stopRunKey struct{}

func stopRun(ctx context.Context) {
	if stop, ok := ctx.Value(stopRunKey{}).(func()); ok {
		stop()
	}
}

// Execute runs the root command. The first Ctrl-C or SIGTERM cancels the
// command's context so it can print what it has; a second one kills it.
func Execute() {
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := func() { cancel(&signaled{at: time.Now()}) }
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		stop()
	}()
	restoreDefault := func() { signal.Stop(sigs) }
	done := make(chan struct{})
	go exitAfterGrace(ctx, done, restoreDefault, rootCmd.ErrOrStderr(), os.Exit)
	code := run(context.WithValue(ctx, stopRunKey{}, stop))
	close(done)
	restoreDefault()
	os.Exit(code)
}

// exitAfterGrace waits for the first signal, restores the default handling
// so that a second one kills the process, and exits 130 if the command is
// still running interruptGrace after the signal, as it is when blocked on a
// full pipe.
func exitAfterGrace(ctx context.Context, done <-chan struct{}, stop func(), stderr io.Writer, exit func(int)) {
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	stop()
	select {
	case <-done:
	case <-time.After(time.Until(exitTime(ctx))):
		// stderr may be the full pipe, so the message gets a moment, not a promise.
		go clierrors.FprintError(stderr, &clierrors.InterruptedError{
			Message: fmt.Sprintf("Stopped %s after the signal; the output may be cut off", interruptGrace),
		})
		time.Sleep(100 * time.Millisecond)
		exit(130)
	}
}

// run executes the root command, reports any error on its stderr as JSON and
// returns the exit code. A request stopped by canceling ctx exits 130; an
// error from what had already arrived, such as a failing --jq, keeps its own.
func run(ctx context.Context) int {
	initHelpCommand()
	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		err = &clierrors.InterruptedError{Message: "Stopped before the response arrived"}
	}
	clierrors.FprintError(rootCmd.ErrOrStderr(), err)
	return clierrors.ExitCode(err)
}

// initHelpCommand keeps cobra's help command, with its text and completion,
// but makes an unknown topic a usage error; cobra prints the usage and exits 0.
// It runs once every command has been added, which cobra needs.
func initHelpCommand() {
	rootCmd.InitDefaultHelpCmd()
	if help, _, err := rootCmd.Find([]string{"help"}); err == nil && help != rootCmd {
		help.RunE = runHelp
	}
}

func runHelp(c *cobra.Command, args []string) error {
	target, _, err := c.Root().Find(args)
	if len(args) > 0 && (err != nil || target == c.Root()) {
		msg := fmt.Sprintf("Unknown help topic %q", strings.Join(args, " "))
		if s := c.Root().SuggestionsFor(args[0]); len(s) > 0 {
			return &clierrors.UsageError{Message: msg + "; did you mean " + strings.Join(s, " or ") + "?"}
		}
		return &clierrors.UsageError{Message: msg + "; see 'serpapi --help'"}
	}
	target.InitDefaultHelpFlag()
	target.InitDefaultVersionFlag()
	return target.Help()
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&apiKeyFlag, "api-key", "", "SerpApi API key; prefer SERPAPI_KEY, since flags show up in ps and shell history")
	pf.StringVar(&fieldsFlag, "fields", "", `search only: server-side json_restrictor, e.g. "organic_results[].{title, link}"`)
	pf.StringVar(&jqFlag, "jq", "", `jq filter applied to the JSON result, e.g. ".organic_results[].link"`)
	pf.StringVar(&timeoutFlag, "timeout", "", fmt.Sprintf(
		"HTTP request timeout in seconds, such as 120, or a duration, such as 2m; 0 waits indefinitely (default %d; env SERPAPI_TIMEOUT)",
		int(api.DefaultTimeout/time.Second)))
	pf.BoolVar(&debugFlag, "debug", false,
		"trace each request on stderr: DNS, connect, TLS, first byte, headers and the server timeline; JSON lines off a terminal (env SERPAPI_DEBUG=1)")

	rootCmd.PersistentPreRunE = validateGlobalFlags

	// Wrap cobra flag-parsing errors as UsageError so they get exit code 2.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &clierrors.UsageError{Message: flagErrorHint(cmd, err.Error())}
	})
}

// flagErrorHint explains the two flag errors agents hit most: a search word
// starting with "-", such as -site:example.com, and an API parameter guessed
// as a flag, such as --num 10.
func flagErrorHint(cmd *cobra.Command, msg string) string {
	if cmd != searchCmd && cmd != locationsCmd {
		return msg
	}
	params := "API parameters are key=value pairs, e.g. engine=google q=coffee"
	if cmd == locationsCmd {
		params = "API parameters are key=value pairs, e.g. q=Austin limit=3"
	}
	if strings.HasPrefix(msg, "unknown shorthand flag") {
		// pflag reports "unknown shorthand flag: 'e' in -e". A one-letter flag
		// is a guessed parameter; a longer one may be a word like -site:x.com.
		if _, arg, _ := strings.Cut(msg, " in "); len(arg) == 2 {
			return msg + "; " + params
		}
		return msg + `; to search for a word starting with "-", put it inside q="..." or after --; ` + params
	}
	if name, ok := strings.CutPrefix(msg, "unknown flag: --"); ok {
		return msg + "; API parameters are key=value pairs, e.g. " + name + "=VALUE"
	}
	return msg
}

// validateGlobalFlags rejects bad flags before any request, so a typo never
// costs a search.
func validateGlobalFlags(cmd *cobra.Command, _ []string) error {
	timeout, err := resolveTimeout()
	if err != nil {
		return err
	}
	requestTimeout = timeout
	debugRequests = resolveDebug()
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

// resolveDebug returns --debug, else whether SERPAPI_DEBUG is set to anything
// but an off value (empty, 0, false, no, off).
func resolveDebug() bool {
	if debugFlag {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SERPAPI_DEBUG"))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
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
	opts := []api.Option{api.WithTimeout(requestTimeout)}
	if debugRequests {
		// Off a terminal, stderr carries only JSON lines, so the trace does too.
		stderr := rootCmd.ErrOrStderr()
		opts = append(opts, api.WithDebug(stderr, !output.IsTerminal(stderr)))
	}
	return api.New(apiKey, append(opts, clientOptions...)...)
}

// withSpinner shows a spinner on an interactive stderr while fn runs and
// clears it before the caller prints anything. In debug mode the trace
// replaces it, so its redraws don't interleave with the trace lines.
func withSpinner[T any](label string, fn func() (T, error)) (T, error) {
	if debugRequests {
		return fn()
	}
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
// next, if not nil, gives the large-result hint its next step.
func handleOutput(cmd *cobra.Command, raw []byte, next func(id string) string) error {
	if jqFlag == "" {
		w := cmd.OutOrStdout()
		out := output.FormatJSON(w, raw)
		if next != nil {
			hintLargeJSON(cmd, out, next)
		}
		_, err := w.Write(out)
		return outputError(err)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return &clierrors.APIError{Message: "Failed to parse response: " + err.Error()}
	}
	ctx, cancel := printContext(cmd.Context())
	defer cancel()
	return printJQ(ctx, cmd, v)
}

// printContext is the context for printing a result that has arrived. After
// a signal the search was billed, so the result still prints, with --jq
// stopped a quarter of interruptGrace before the process exits: that leaves
// it time to flush and report what it printed.
func printContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil || ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithDeadline(context.WithoutCancel(ctx), exitTime(ctx).Add(-interruptGrace/4))
}

// printJQ runs --jq on v until ctx ends, printing each result as it is
// produced. A filter that prints only null gets a warning listing the keys v
// has.
func printJQ(ctx context.Context, cmd *cobra.Command, v any) error {
	code, err := jq.Compile(jqFlag)
	if err != nil {
		return &clierrors.UsageError{Message: err.Error()}
	}
	w := cmd.OutOrStdout()
	tty := output.IsTerminal(w)
	bw := bufio.NewWriter(w)
	var writeErr error
	printed, nulls := 0, 0
	if ctx == nil {
		ctx = context.Background()
	}
	err = jq.Run(ctx, code, v, func(val any) error {
		printed++
		if val == nil {
			nulls++
		}
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
		return &clierrors.UsageError{Message: jqErrorHint(err.Error(), v)}
	}
	if keys := objectKeys(v); printed > 0 && nulls == printed && keys != "" {
		warn(cmd, "--jq printed only null; check the path against the response's top-level keys: %s", keys)
	}
	return nil
}

// jqErrorHint adds the fix for the jq errors agents hit most: a value of
// another type than the filter expects, and a path missing from this
// response, for which it lists the keys the response does have.
func jqErrorHint(msg string, v any) string {
	if strings.Contains(msg, "expected an object but got") {
		return msg + "; add ? to skip values of another type, e.g. .source.name? // .source"
	}
	if !strings.Contains(msg, ": null") {
		return msg
	}
	if strings.Contains(msg, "cannot iterate over: null") {
		msg += "; use []? to skip a missing array, e.g. .organic_results[]?"
	}
	if obj, ok := v.(map[string]any); ok && len(obj) > 0 {
		msg += "; the response has: " + strings.Join(slices.Sorted(maps.Keys(obj)), ", ")
	}
	return msg
}

func outputError(err error) error {
	if err == nil {
		return nil
	}
	return &clierrors.APIError{Message: "Output error: " + err.Error()}
}

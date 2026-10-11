package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/serpapi/serpapi-cli/pkg/api"
	"github.com/serpapi/serpapi-cli/pkg/config"
)

type result struct {
	code           int
	stdout, stderr string
}

type requests struct {
	mu   sync.Mutex
	urls []*url.URL
}

func (r *requests) add(u *url.URL) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls = append(r.urls, u)
}

func (r *requests) list() []*url.URL {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*url.URL(nil), r.urls...)
}

func (r *requests) only(t *testing.T) *url.URL {
	t.Helper()
	urls := r.list()
	if len(urls) != 1 {
		t.Fatalf("got %d requests, want 1: %v", len(urls), urls)
	}
	return urls[0]
}

// fakeAPI isolates the environment and the config directory and points the
// CLI at handler. A nil handler fails the test on any request.
func fakeAPI(t *testing.T, handler http.HandlerFunc) *requests {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("AppData", home)
	t.Setenv("NO_COLOR", "1")
	for _, name := range apiKeyEnvVars {
		t.Setenv(name, "")
	}

	reqs := &requests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.add(r.URL)
		if handler == nil {
			t.Errorf("unexpected request to %s", r.URL.Path)
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	clientOptions = []api.Option{api.WithBaseURL(srv.URL)}
	t.Cleanup(func() { clientOptions = nil })
	return reqs
}

func respond(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }
}

func respondStatus(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

// execute runs the CLI in-process with every flag back at its default, as in
// a new process.
func execute(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	return executeContext(t, context.Background(), stdin, args...)
}

// executeContext is execute with a context the test can cancel, as Ctrl-C does.
func executeContext(t *testing.T, ctx context.Context, stdin string, args ...string) result {
	t.Helper()
	resetFlags(rootCmd)
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetIn(strings.NewReader(stdin))
	// A nil slice would make cobra parse the test binary's own arguments.
	rootCmd.SetArgs(append([]string{}, args...))
	code := run(ctx)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// resetFlags puts every flag back at its default and clears each command's
// context, which cobra otherwise keeps from the first run in this process.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	//lint:ignore SA1012 cobra hands a command the run's context only while its own is nil
	c.SetContext(nil)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// parseStderr requires every stderr line to be a JSON error or warning and
// returns the warning messages and the error, if any.
func parseStderr(t *testing.T, stderr string) ([]string, map[string]any) {
	t.Helper()
	var warnings []string
	var errObj map[string]any
	for _, line := range strings.Split(stderr, "\n") {
		if line == "" {
			continue
		}
		var v struct {
			Error   map[string]any `json:"error"`
			Warning *struct {
				Message string `json:"message"`
			} `json:"warning"`
		}
		if err := json.Unmarshal([]byte(line), &v); err != nil || (v.Error == nil && v.Warning == nil) {
			t.Fatalf("stderr line is not a JSON error or warning: %q", line)
		}
		if v.Warning != nil {
			warnings = append(warnings, v.Warning.Message)
		}
		if v.Error != nil {
			if errObj != nil {
				t.Fatalf("more than one error on stderr: %q", stderr)
			}
			errObj = v.Error
		}
	}
	return warnings, errObj
}

func wantOK(t *testing.T, res result) []string {
	t.Helper()
	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", res.code, res.stderr)
	}
	warnings, errObj := parseStderr(t, res.stderr)
	if errObj != nil {
		t.Fatalf("unexpected error: %v", errObj)
	}
	return warnings
}

func wantError(t *testing.T, res result, exit int, code, msgPart string) map[string]any {
	t.Helper()
	if res.code != exit {
		t.Fatalf("exit code = %d, want %d; stderr: %s", res.code, exit, res.stderr)
	}
	_, errObj := parseStderr(t, res.stderr)
	if errObj == nil {
		t.Fatalf("no error on stderr: %q", res.stderr)
	}
	if errObj["code"] != code {
		t.Errorf("error code = %v, want %s", errObj["code"], code)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, msgPart) {
		t.Errorf("error message = %q, want it to contain %q", msg, msgPart)
	}
	return errObj
}

func wantWarnings(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings = %q, want %q", got, want)
	}
}

func wantQuery(t *testing.T, u *url.URL, path string, want map[string]string) {
	t.Helper()
	if u.Path != path {
		t.Errorf("path = %s, want %s", u.Path, path)
	}
	q := u.Query()
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestSearchSendsParamsAndPrintsIndentedJSON(t *testing.T) {
	reqs := fakeAPI(t, respond(`{"organic_results": [{"title": "Café <b>Blue</b> & co"}]}`))
	t.Setenv("SERPAPI_API_KEY", "altkey")

	res := execute(t, "", "search", "engine=google", "coffee", "shops", "no_cache=true")

	wantWarnings(t, wantOK(t, res))
	if want := "{\n  \"organic_results\": [\n    {\n      \"title\": \"Café <b>Blue</b> & co\"\n    }\n  ]\n}\n"; res.stdout != want {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}
	wantQuery(t, reqs.only(t), "/search.json", map[string]string{
		"engine": "google", "q": "coffee shops", "no_cache": "true", "api_key": "altkey",
	})
}

func TestAPIKeyPrecedence(t *testing.T) {
	tests := []struct {
		name, flag, serpapiKey, serpapiAPIKey, want string
	}{
		{"flag", "flagkey", "envkey", "altkey", "flagkey"},
		{"SERPAPI_KEY", "", "envkey", "altkey", "envkey"},
		{"SERPAPI_API_KEY", "", "  ", "altkey", "altkey"},
		{"saved", "", "", "", "savedkey"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs := fakeAPI(t, respond(`{"account_email":"dev@example.com"}`))
			if err := config.SaveConfig("savedkey"); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SERPAPI_KEY", tt.serpapiKey)
			t.Setenv("SERPAPI_API_KEY", tt.serpapiAPIKey)
			args := []string{"account"}
			if tt.flag != "" {
				args = append(args, "--api-key", tt.flag)
			}

			wantOK(t, execute(t, "", args...))
			wantQuery(t, reqs.only(t), "/account.json", map[string]string{"api_key": tt.want})
		})
	}
}

func TestFieldsSendsRestrictorAndWarnsWhenNothingMatches(t *testing.T) {
	reqs := fakeAPI(t, respond(`{}`))
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--fields", "organic_results[0:3].{title, link}")

	wantWarnings(t, wantOK(t, res),
		"json_restrictor matched nothing or is malformed; compare its paths with the unfiltered response")
	if res.stdout != "{}\n" {
		t.Errorf("stdout = %q, want {}", res.stdout)
	}
	wantQuery(t, reqs.only(t), "/search.json", map[string]string{
		"json_restrictor": "organic_results[0:3].{title, link},error",
	})
}

func TestJQPrintsStringsRawAndKeepsNumbers(t *testing.T) {
	fakeAPI(t, respond(`{"organic_results":[{"link":"https://a.example/?x=1&y=<2>"},{"link":"https://b.example"}],`+
		`"search_information":{"total_results":12345678901234567890}}`))
	t.Setenv("SERPAPI_KEY", "envkey")

	tests := []struct{ filter, want string }{
		{".organic_results[].link", "https://a.example/?x=1&y=<2>\nhttps://b.example\n"},
		{".search_information.total_results", "12345678901234567890\n"},
		{".organic_results[0]", `{"link":"https://a.example/?x=1&y=<2>"}` + "\n"},
	}
	for _, tt := range tests {
		res := execute(t, "", "search", "--jq", tt.filter, "engine=google", "q=coffee")
		wantOK(t, res)
		if res.stdout != tt.want {
			t.Errorf("--jq %s: stdout = %q, want %q", tt.filter, res.stdout, tt.want)
		}
	}
}

// page returns results page n of a search; every page but the last links to
// the next one, as SerpApi does, without the parameters it drops.
func page(n int, hasNext bool) string {
	next := ""
	if hasNext {
		next = fmt.Sprintf(`,"serpapi_pagination":{"next":"https://serpapi.com/search.json?engine=google&q=coffee&start=%d"}`, n*10)
	}
	return fmt.Sprintf(`{"search_metadata":{"id":"page%d"},"organic_results":[{"position":%d}]%s}`, n, n, next)
}

func pageNumber(r *http.Request) int {
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	return start/10 + 1
}

func organicResults(t *testing.T, stdout string) []any {
	t.Helper()
	var merged map[string]any
	if err := json.Unmarshal([]byte(stdout), &merged); err != nil {
		t.Fatalf("stdout is not one JSON object: %v: %q", err, stdout)
	}
	if _, ok := merged["serpapi_pagination"]; ok {
		t.Errorf("merged result keeps serpapi_pagination")
	}
	results, _ := merged["organic_results"].([]any)
	return results
}

func TestAllPagesMergesPagesAndCarriesParams(t *testing.T) {
	reqs := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		n := pageNumber(r)
		fmt.Fprint(w, page(n, n < 3))
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "no_cache=true",
		"--fields", "organic_results", "--all-pages")

	wantWarnings(t, wantOK(t, res))
	if got := organicResults(t, res.stdout); len(got) != 3 {
		t.Errorf("merged %d results, want 3", len(got))
	}
	urls := reqs.list()
	if len(urls) != 3 {
		t.Fatalf("got %d requests, want 3", len(urls))
	}
	for i, u := range urls {
		start := ""
		if i > 0 {
			start = strconv.Itoa(i * 10)
		}
		wantQuery(t, u, "/search.json", map[string]string{
			"engine": "google", "q": "coffee", "start": start, "no_cache": "true", "api_key": "envkey",
			"json_restrictor": "organic_results,error,serpapi_pagination.next",
		})
	}
}

func TestAllPagesStopsAtMaxPages(t *testing.T) {
	reqs := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page(pageNumber(r), true))
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--all-pages")
	wantWarnings(t, wantOK(t, res),
		"Stopped at 100 pages, but more exist; raise --max-pages to fetch them (one search each)")
	if got := len(reqs.list()); got != 100 {
		t.Errorf("default cap made %d requests, want 100", got)
	}

	res = execute(t, "", "search", "engine=google", "q=coffee", "--all-pages", "--max-pages", "2")
	wantWarnings(t, wantOK(t, res))
	if got := len(organicResults(t, res.stdout)); got != 2 {
		t.Errorf("merged %d results, want 2", got)
	}
	if got := len(reqs.list()); got != 102 {
		t.Errorf("--max-pages 2 made %d requests, want 2", got-100)
	}
}

func TestAllPagesStopsWhenNextPageRepeats(t *testing.T) {
	reqs := fakeAPI(t, respond(page(1, true)))
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--all-pages")

	wantWarnings(t, wantOK(t, res), "Stopped after page 2: the next page repeats an earlier one")
	if got := len(reqs.list()); got != 2 {
		t.Errorf("got %d requests, want 2", got)
	}
}

func TestAllPagesTreatsNoResultsAsTheEnd(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if pageNumber(r) == 2 {
			fmt.Fprint(w, `{"error":"Google hasn't returned any results for this query."}`)
			return
		}
		fmt.Fprint(w, page(1, true))
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--all-pages")

	wantWarnings(t, wantOK(t, res), "Stopped after page 1: Google hasn't returned any results for this query.")
	if got := len(organicResults(t, res.stdout)); got != 1 {
		t.Errorf("merged %d results, want 1", got)
	}
}

func TestAllPagesPrintsEarlierPagesWhenALaterOneFails(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if pageNumber(r) == 3 {
			respondStatus(429, `{"error":"Your account has run out of searches."}`)(w, r)
			return
		}
		fmt.Fprint(w, page(pageNumber(r), true))
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--all-pages")

	errObj := wantError(t, res, 1, "api_error",
		"Your account has run out of searches. (page 3 failed; pages 1-2 were printed)")
	if errObj["status"] != float64(429) {
		t.Errorf("status = %v, want 429", errObj["status"])
	}
	if got := len(organicResults(t, res.stdout)); got != 2 {
		t.Errorf("merged %d results, want 2", got)
	}
}

func TestAllPagesStopsWhenFieldsMatchNothing(t *testing.T) {
	reqs := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"serpapi_pagination":{"next":"https://serpapi.com/search.json?engine=google&q=coffee&start=%d"}}`, pageNumber(r)*10)
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--fields", "organic_result", "--all-pages")

	wantWarnings(t, wantOK(t, res),
		"json_restrictor matched nothing or is malformed; compare its paths with the unfiltered response")
	if res.stdout != "{}\n" {
		t.Errorf("stdout = %q, want {}", res.stdout)
	}
	reqs.only(t)
}

func TestAllPagesStopsAtAPageFieldsMatchNothingOn(t *testing.T) {
	reqs := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if n := pageNumber(r); n != 2 {
			fmt.Fprint(w, page(n, true))
			return
		}
		fmt.Fprint(w, `{"serpapi_pagination":{"next":"https://serpapi.com/search.json?engine=google&q=coffee&start=20"}}`)
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--fields", "organic_results", "--all-pages")

	wantWarnings(t, wantOK(t, res),
		"Stopped after page 2: nothing on it matched json_restrictor; to fetch later pages anyway, filter with --jq instead of --fields")
	if got := len(organicResults(t, res.stdout)); got != 1 {
		t.Errorf("merged %d results, want 1", got)
	}
	if got := len(reqs.list()); got != 2 {
		t.Errorf("got %d requests, want 2", got)
	}
}

func TestMaxPagesWithoutAllPagesWarns(t *testing.T) {
	reqs := fakeAPI(t, respond(page(1, true)))
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "search", "engine=google", "q=coffee", "--max-pages", "3")

	wantWarnings(t, wantOK(t, res), "--max-pages has no effect without --all-pages")
	reqs.only(t)
}

func TestAccountMasksAPIKey(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	fakeAPI(t, respond(`{"account_email":"dev@example.com","api_key":"`+key+`","total_searches_left":42}`))
	t.Setenv("SERPAPI_KEY", key)

	res := execute(t, "", "account")
	wantOK(t, res)
	if strings.Contains(res.stdout, key) || !strings.Contains(res.stdout, `"api_key": "0123…cdef"`) {
		t.Errorf("stdout does not mask the key: %s", res.stdout)
	}

	res = execute(t, "", "account", "--jq", ".total_searches_left")
	wantOK(t, res)
	if res.stdout != "42\n" {
		t.Errorf("stdout = %q, want 42", res.stdout)
	}
}

func TestArchiveOutputFormats(t *testing.T) {
	reqs := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("output") {
		case "md":
			fmt.Fprint(w, "# Coffee")
		case "rank:example.com":
			fmt.Fprint(w, "3")
		default:
			fmt.Fprint(w, `{"search_metadata":{"id":"abc_123-X"},"organic_results":[{"link":"https://example.com"}]}`)
		}
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	tests := []struct {
		args           []string
		output, stdout string
	}{
		{nil, "", "{\n  \"search_metadata\": {\n    \"id\": \"abc_123-X\"\n  },\n  \"organic_results\": [\n    {\n      \"link\": \"https://example.com\"\n    }\n  ]\n}\n"},
		{[]string{"output=md"}, "md", "# Coffee\n"},
		{[]string{"output=rank:example.com"}, "rank:example.com", "3\n"},
		{[]string{"output=json_with_images", "--jq", ".organic_results[0].link"}, "json_with_images", "https://example.com\n"},
	}
	for i, tt := range tests {
		res := execute(t, "", append([]string{"archive", "abc_123-X"}, tt.args...)...)
		wantOK(t, res)
		if res.stdout != tt.stdout {
			t.Errorf("%v: stdout = %q, want %q", tt.args, res.stdout, tt.stdout)
		}
		urls := reqs.list()
		if len(urls) != i+1 {
			t.Fatalf("%v: got %d requests in total, want %d", tt.args, len(urls), i+1)
		}
		wantQuery(t, urls[i], "/searches/abc_123-X.json", map[string]string{"output": tt.output, "api_key": "envkey"})
	}
}

func TestLocationsSendsNoAPIKey(t *testing.T) {
	reqs := fakeAPI(t, respond(`[{"canonical_name":"Austin,TX,Texas,United States"}]`))
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", "locations", "Austin", "limit=3", "--jq", ".[0].canonical_name")

	wantOK(t, res)
	if res.stdout != "Austin,TX,Texas,United States\n" {
		t.Errorf("stdout = %q", res.stdout)
	}
	u := reqs.only(t)
	wantQuery(t, u, "/locations.json", map[string]string{"q": "Austin", "limit": "3"})
	if u.Query().Has("api_key") {
		t.Errorf("locations sent an API key: %s", u.RawQuery)
	}
}

func TestLoginSavesPipedKey(t *testing.T) {
	reqs := fakeAPI(t, respond(`{"account_email":"dev@example.com"}`))
	if err := config.SaveConfig("oldkey"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERPAPI_API_KEY", "altkey")

	res := execute(t, "  newkey  \nignored\n", "login")

	wantWarnings(t, wantOK(t, res), "SERPAPI_API_KEY is set and takes precedence over the saved key")
	if key, _ := config.LoadAPIKey(); key != "newkey" {
		t.Errorf("saved key = %q, want newkey", key)
	}
	wantQuery(t, reqs.only(t), "/account.json", map[string]string{"api_key": "newkey"})
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
}

func TestLoginRewritesABrokenConfig(t *testing.T) {
	fakeAPI(t, respond(`{"account_email":"dev@example.com"}`))
	writeConfig("api_key = " + strayKey)(t)

	res := execute(t, "newkey\n", "login")

	wantOK(t, res)
	if key, err := config.LoadAPIKey(); key != "newkey" || err != nil {
		t.Errorf("saved key = %q, %v; want newkey", key, err)
	}
}

func TestLoginWithFlagSkipsStdin(t *testing.T) {
	reqs := fakeAPI(t, respond(`{"account_email":"dev@example.com"}`))

	res := execute(t, "stdinkey\n", "login", "--api-key", "flagkey")

	if res.code != 0 {
		t.Fatalf("exit code = %d; stderr: %s", res.code, res.stderr)
	}
	if key, _ := config.LoadAPIKey(); key != "flagkey" {
		t.Errorf("saved key = %q, want flagkey", key)
	}
	wantQuery(t, reqs.only(t), "/account.json", map[string]string{"api_key": "flagkey"})
}

func TestLoginRejectsInvalidKeyWithoutSaving(t *testing.T) {
	fakeAPI(t, respondStatus(401, `{"error":"Invalid API key."}`))

	res := execute(t, "badkey\n", "login")

	errObj := wantError(t, res, 1, "api_error", "Invalid API key.")
	if errObj["status"] != float64(401) {
		t.Errorf("status = %v, want 401", errObj["status"])
	}
	if key, _ := config.LoadAPIKey(); key != "" {
		t.Error("an invalid key was saved")
	}
}

func TestLoginWithoutKey(t *testing.T) {
	fakeAPI(t, nil)
	wantError(t, execute(t, "\n", "login"), 2, "usage_error", "No API key given")
}

func TestNoCommandPrintsHelpToStderr(t *testing.T) {
	fakeAPI(t, nil)

	res := execute(t, "")

	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2", res.code)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
	help, last, _ := strings.Cut(strings.TrimSuffix(res.stderr, "\n"), "\n{")
	if !strings.Contains(help, "Usage:") {
		t.Errorf("stderr has no usage: %q", res.stderr)
	}
	if _, errObj := parseStderr(t, "{"+last); errObj["message"] != "No command given; see 'serpapi --help'" {
		t.Errorf("error = %v", errObj)
	}
}

func TestHelpAndVersionGoToStdout(t *testing.T) {
	fakeAPI(t, nil)

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "SERPAPI_KEY"},
		{[]string{"search", "--help"}, "--all-pages"},
		{[]string{"--version"}, "serpapi version "},
	} {
		res := execute(t, "", tt.args...)
		if res.code != 0 || res.stderr != "" || !strings.Contains(res.stdout, tt.want) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want %q on stdout", tt.args, res.code, res.stdout, res.stderr, tt.want)
		}
	}
}

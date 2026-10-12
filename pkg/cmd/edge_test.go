package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/serpapi/serpapi-cli/pkg/config"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

// edgeCase is one edge case of the CLI. Cases are grouped by the one input
// that varies while the rest stays at its default (SERPAPI_KEY=envkey and an
// API that answers with one result), so each case has exactly one home:
//
//	argv      positional parameters: shape, encoding, reserved names
//	flags     flag values and combinations
//	auth      where the API key comes from
//	response  the API's answer: status, body shape, transport
//	paging    the serpapi_pagination.next chain of --all-pages
//	output    how results reach stdout
//
// Flows with several steps (login, key precedence, page merging, help) have
// their own tests in cmd_test.go.
type edgeCase struct {
	name    string
	args    []string
	env     map[string]string
	setup   func(t *testing.T)
	api     http.HandlerFunc // nil: any request fails the test
	exit    int
	code    string // error code, "" on success
	msg     string // substring of the error message
	exact   bool   // msg is the whole error message
	status  int    // error status, 0 for none
	stdout  string // exact stdout when set; errors without it must print nothing
	reqs    int    // requests expected when api is set; 0 means 1
	query   map[string]string
	noQuery []string // parameters every request must omit
	warn    string   // substring of a warning that must be printed
	quiet   bool     // no warning may be printed
}

const strayKey = "secretkey0123456789"

var (
	oneResult = respond(`{"organic_results":[{"position":1}]}`)
	noKey     = map[string]string{"SERPAPI_KEY": "", "SERPAPI_API_KEY": ""}
	search    = []string{"search", "engine=google", "q=coffee"}
)

func searchWith(args ...string) []string { return append(append([]string{}, search...), args...) }

func edgeCases() []edgeCase {
	invalidKey := `{"error":"Invalid API key. Your API key should be here: https://serpapi.com/manage-api-key"}`
	pages := func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page(pageNumber(r), pageNumber(r) < 3)) }
	bigPages := func(w http.ResponseWriter, r *http.Request) {
		next := ""
		if pageNumber(r) == 1 {
			next = `"serpapi_pagination":{"next":"https://serpapi.com/search.json?engine=google&q=coffee&start=10"},`
		}
		fmt.Fprint(w, bigResult(next+`"inline_images":[{"position":1}],`, 700))
	}
	return []edgeCase{
		{name: "argv/no parameters", args: []string{"search"}, exit: 2, code: "usage_error", msg: "No search parameters given"},
		{name: "argv/q and bare words", args: searchWith("tea"), exit: 2, code: "usage_error", msg: "Got both q=... and bare words"},
		{name: "argv/api_key parameter", args: searchWith("api_key=" + strayKey), exit: 2, code: "usage_error", msg: "not as the api_key parameter"},
		{name: "argv/word starting with a dash", args: []string{"search", "engine=google", "coffee", "-starbucks"}, exit: 2, code: "usage_error",
			msg: `unknown shorthand flag: 's' in -starbucks; to search for a word starting with "-", put it inside q="..." or after --`},
		{name: "argv/dash word after --", args: []string{"search", "engine=google", "--", "coffee", "-starbucks"}, api: oneResult,
			query: map[string]string{"q": "coffee -starbucks"}},
		{name: "argv/reserved URL characters", args: []string{"search", "engine=google", "q=a&b=c d+e%"}, api: oneResult,
			query: map[string]string{"q": "a&b=c d+e%"}},
		{name: "argv/repeated key keeps the last", args: searchWith("q=tea"), api: oneResult, query: map[string]string{"q": "tea"},
			warn: `q given more than once; using the last value, "tea"`},
		{name: "argv/repeated identical key is quiet", args: searchWith("engine=google"), api: oneResult, quiet: true},
		{name: "argv/locations repeated key", args: []string{"locations", "q=Austin", "q=Dallas"}, api: respond(`[]`),
			query: map[string]string{"q": "Dallas"}, warn: "q given more than once"},
		{name: "argv/empty value is sent", args: []string{"search", "engine=google", "q="}, api: oneResult, query: map[string]string{"q": ""}},
		{name: "argv/unknown command", args: []string{"nope"}, exit: 2, code: "usage_error", msg: `unknown command "nope"`},
		{name: "argv/stray argument to account", args: []string{"account", strayKey}, exit: 2, code: "usage_error", msg: "account takes no arguments"},
		{name: "argv/stray argument to login", args: []string{"login", strayKey}, exit: 2, code: "usage_error", msg: "login takes no arguments; pipe the key instead"},
		{name: "argv/archive ID with a path", args: []string{"archive", "../bad"}, exit: 2, code: "usage_error", msg: "Invalid archive ID"},
		{name: "argv/archive extra word", args: []string{"archive", "abc123", "md"}, exit: 2, code: "usage_error", msg: "Unexpected argument md"},
		{name: "argv/archive with --jq reads the stored search", args: []string{"archive", "abc123", "--jq", ".place_results | {title, rating}"},
			api:    respond(`{"search_metadata":{"id":"abc123"},"place_results":{"title":"Tartine","rating":4.6,"reviews":7351}}`),
			stdout: `{"rating":4.6,"title":"Tartine"}` + "\n", query: map[string]string{"api_key": "envkey"}},
		{name: "argv/version", args: []string{"version"}, stdout: "serpapi version " + version.Version + "\n", quiet: true},
		{name: "argv/version extra word", args: []string{"version", "1"}, exit: 2, code: "usage_error", msg: "version takes no arguments"},
		{name: "flags/jq on version", args: []string{"version", "--jq", ".version"}, exit: 2, code: "usage_error", msg: "version prints text, not JSON"},
		{name: "argv/locations without query", args: []string{"locations"}, exit: 2, code: "usage_error", msg: "Pass a location query"},
		{name: "argv/locations blank query", args: []string{"locations", "q=  "}, exit: 2, code: "usage_error", msg: "Pass a location query"},

		{name: "flags/unknown flag", args: searchWith("--bogus"), exit: 2, code: "usage_error",
			msg: "unknown flag: --bogus; API parameters are key=value pairs, e.g. bogus=VALUE"},
		{name: "flags/API parameter as a flag", args: searchWith("--num", "10"), exit: 2, code: "usage_error", msg: "e.g. num=VALUE"},
		{name: "flags/one-letter flag guess", args: []string{"search", "-e", "youtube", "--search_query", "Rick"}, exit: 2, code: "usage_error",
			msg: "unknown shorthand flag: 'e' in -e; API parameters are key=value pairs, e.g. engine=google q=coffee", exact: true},
		{name: "flags/unknown flag outside search", args: []string{"account", "--bogus"}, exit: 2, code: "usage_error", msg: "unknown flag: --bogus", exact: true},
		{name: "flags/timeout in seconds", args: searchWith("--timeout", "30"), api: oneResult},
		{name: "flags/timeout as a duration", args: searchWith("--timeout", "2m"), api: oneResult},
		{name: "flags/timeout zero waits indefinitely", args: searchWith("--timeout", "0"), api: oneResult},
		{name: "flags/timeout negative", args: searchWith("--timeout=-5"), exit: 2, code: "usage_error", msg: `Invalid --timeout value "-5"`},
		{name: "flags/timeout infinity", args: searchWith("--timeout", "Inf"), exit: 2, code: "usage_error", msg: `Invalid --timeout value "Inf"`},
		{name: "flags/timeout negative duration", args: searchWith("--timeout=-1s"), exit: 2, code: "usage_error", msg: `Invalid --timeout value "-1s"`},
		{name: "flags/timeout NaN", args: searchWith("--timeout", "NaN"), exit: 2, code: "usage_error", msg: `Invalid --timeout value "NaN"`},
		{name: "flags/timeout past 31 years", args: searchWith("--timeout", "1e10"), exit: 2, code: "usage_error", msg: `Invalid --timeout value "1e10"`},
		{name: "flags/timeout under a nanosecond", args: searchWith("--timeout", "1e-10"), exit: 2, code: "usage_error", msg: `Invalid --timeout value "1e-10"`},
		{name: "flags/timeout duration under a nanosecond", args: searchWith("--timeout", "0.5ns"), exit: 2, code: "usage_error",
			msg: `Invalid --timeout value "0.5ns"`},
		{name: "flags/timeout 0s waits indefinitely", args: searchWith("--timeout", "0s"), api: oneResult},
		{name: "flags/timeout word", args: searchWith("--timeout", "soon"), exit: 2, code: "usage_error",
			msg: "expected seconds, such as 120, or a duration, such as 2m; 0 waits indefinitely"},
		{name: "flags/SERPAPI_TIMEOUT not a number", args: search, env: map[string]string{"SERPAPI_TIMEOUT": "abc"}, exit: 2, code: "usage_error",
			msg: `Invalid SERPAPI_TIMEOUT value "abc"`},
		{name: "flags/--timeout wins over SERPAPI_TIMEOUT", args: searchWith("--timeout", "30"), env: map[string]string{"SERPAPI_TIMEOUT": "abc"}, api: oneResult},
		{name: "flags/max-pages zero", args: searchWith("--all-pages", "--max-pages", "0"), exit: 2, code: "usage_error", msg: "--max-pages must be at least 1"},
		{name: "flags/all-pages with async", args: searchWith("async=true", "--all-pages"), exit: 2, code: "usage_error", msg: "drop async=true"},
		{name: "flags/fields outside search", args: []string{"account", "--fields", "plan_name"}, exit: 2, code: "usage_error", msg: "--fields works only with search"},
		{name: "flags/fields on locations", args: []string{"locations", "q=Austin", "--fields", "name"}, exit: 2, code: "usage_error", msg: "--fields works only with search"},
		{name: "flags/jq parse error", args: []string{"search", "--jq", ".[", "engine=google", "q=coffee"}, exit: 2, code: "usage_error", msg: "jq parse error"},
		{name: "flags/jq input builtin", args: searchWith("--jq", "input"), exit: 2, code: "usage_error", msg: "is not allowed"},
		{name: "flags/jq iterates a missing array", args: searchWith("--jq", ".missing[].link"), api: oneResult, exit: 2, code: "usage_error",
			msg: "use []? to skip a missing array, e.g. .organic_results[]?; the response has: organic_results"},
		{name: "flags/jq on a missing object lists the keys", args: searchWith("--jq", ".place_results | keys"),
			api: respond(`{"search_metadata":{"id":"1"},"local_results":[{"title":"Blue Bottle"}]}`), exit: 2, code: "usage_error",
			msg: "jq runtime error: keys cannot be applied to: null; the response has: local_results, search_metadata", exact: true},
		{name: "flags/jq type mismatch suggests ?", args: searchWith("--jq", "[.news_results[] | {title, source: (.source.name // .source)}]"),
			api: respond(`{"news_results":[{"title":"a","source":"Computer Weekly"}]}`), exit: 2, code: "usage_error",
			msg: "; add ? to skip values of another type, e.g. .source.name? // .source"},
		{name: "flags/the suggested ? fix works", args: searchWith("--jq", "[.news_results[] | {title, source: (.source.name? // .source)}]"),
			api:    respond(`{"news_results":[{"title":"a","source":"Computer Weekly"},{"title":"b","source":{"name":"Wired"}}]}`),
			stdout: `[{"source":"Computer Weekly","title":"a"},{"source":"Wired","title":"b"}]` + "\n"},
		{name: "flags/the one-item preview keeps every key", args: searchWith("--jq", `walk(if type == "array" then .[:1] else . end)`),
			api:    respond(`{"organic_results":[{"title":"a"},{"title":"b"}],"related_searches":[{"query":"x"},{"query":"y"}]}`),
			stdout: `{"organic_results":[{"title":"a"}],"related_searches":[{"query":"x"}]}` + "\n"},
		{name: "flags/jq cannot read the environment", args: searchWith("--jq", "[$ENV, env] | map(length)"), api: oneResult, stdout: "[0,0]\n"},
		{name: "flags/output md", args: searchWith("output=md"), api: respond("# Coffee\n\n1. [Blue Bottle](https://bluebottlecoffee.com)"),
			stdout: "# Coffee\n\n1. [Blue Bottle](https://bluebottlecoffee.com)\n", query: map[string]string{"output": "md"}},
		{name: "flags/output in capitals", args: searchWith("output=MD"), api: respond("# Coffee\n"), stdout: "# Coffee\n",
			query: map[string]string{"output": "MD"}},
		{name: "flags/unknown output", args: searchWith("output=xml"), exit: 2, code: "usage_error", msg: "Unknown output=xml"},
		{name: "flags/rank output without domain", args: searchWith("output=rank:"), exit: 2, code: "usage_error", msg: "Unknown output=rank:"},
		{name: "flags/jq with markdown", args: searchWith("output=md", "--jq", ".x"), exit: 2, code: "usage_error", msg: "--jq needs JSON, but output=md is not JSON"},
		{name: "flags/all-pages with markdown", args: searchWith("output=md", "--all-pages"), exit: 2, code: "usage_error", msg: "--all-pages needs JSON"},
		{name: "flags/archive unknown output", args: []string{"archive", "abc123", "output=xml"}, exit: 2, code: "usage_error", msg: "Unknown output=xml"},
		{name: "flags/archive jq with markdown", args: []string{"archive", "abc123", "output=md", "--jq", ".x"}, exit: 2, code: "usage_error", msg: "--jq needs JSON"},

		{name: "auth/no key for account", args: []string{"account"}, env: noKey, exit: 2, code: "usage_error", msg: "No API key found"},
		{name: "auth/no key for archive", args: []string{"archive", "abc123"}, env: noKey, exit: 2, code: "usage_error", msg: "No API key found"},
		{name: "auth/no key for search", args: search, env: noKey, api: oneResult, noQuery: []string{"api_key"}},
		{name: "auth/no key for all pages", args: searchWith("--all-pages"), env: noKey, api: oneResult, noQuery: []string{"api_key"}},
		{name: "auth/no key for markdown", args: searchWith("output=md"), env: noKey, api: respond("# Coffee\n"), noQuery: []string{"api_key"}},
		{name: "auth/no key and a metered query", args: search, env: noKey, api: respondStatus(401, invalidKey), exit: 1, code: "api_error", status: 401,
			msg: "manage-api-key (no API key found; pass --api-key, set SERPAPI_KEY or run 'serpapi login')"},
		{name: "auth/invalid key keeps the API's message", args: search, env: map[string]string{"SERPAPI_KEY": "badkey"}, api: respondStatus(401, invalidKey),
			exit: 1, code: "api_error", status: 401, msg: "Invalid API key. Your API key should be here: https://serpapi.com/manage-api-key", exact: true},
		{name: "auth/padded key is trimmed", args: []string{"account"}, env: map[string]string{"SERPAPI_KEY": "  padded\n"},
			api: respond(`{"account_status":"Active"}`), query: map[string]string{"api_key": "padded"}},
		{name: "auth/config not valid TOML", args: []string{"account"}, env: noKey, setup: writeConfig("api_key = " + strayKey),
			exit: 2, code: "usage_error", msg: "is not valid TOML (line 1); run 'serpapi login' to rewrite it"},
		{name: "auth/env key wins over a broken config", args: []string{"account"}, setup: writeConfig("api_key = " + strayKey),
			api: respond(`{"account_status":"Active"}`), query: map[string]string{"api_key": "envkey"}},
		{name: "auth/search warns about a broken config", args: search, env: noKey, setup: writeConfig("api_key = " + strayKey),
			api: oneResult, noQuery: []string{"api_key"}, warn: "is not valid TOML (line 1); run 'serpapi login' to rewrite it, or pass --api-key or SERPAPI_KEY; searching without a key"},
		{name: "auth/config unreadable", args: []string{"account"}, env: noKey, setup: unreadableConfig,
			exit: 2, code: "usage_error", msg: "Cannot read the config file"},

		{name: "response/invalid key", args: search, api: respondStatus(401, `{"error":"Invalid API key."}`), exit: 1, code: "api_error", status: 401, msg: "Invalid API key."},
		{name: "response/out of searches", args: search, api: respondStatus(429, `{"error":"Your account has run out of searches."}`),
			exit: 1, code: "api_error", status: 429, msg: "run out of searches"},
		{name: "response/HTML error page", args: search, api: respondStatus(404, `<html>Not Found</html>`), exit: 1, code: "api_error", status: 404, msg: "HTTP 404: Not Found"},
		{name: "response/no results", args: search, api: respond(`{"error":"Google hasn't returned any results for this query."}`),
			exit: 1, code: "api_error", status: 200, msg: "hasn't returned any results"},
		{name: "response/no results as markdown", args: searchWith("output=md"), api: respond(`{"error":"Google hasn't returned any results for this query."}`),
			exit: 1, code: "api_error", status: 200, msg: "hasn't returned any results"},
		{name: "response/null body", args: search, api: respond(`null`), exit: 1, code: "api_error", msg: "not a JSON object"},
		{name: "response/array body", args: search, api: respond(`[{"position":1}]`), exit: 1, code: "api_error", msg: "not a JSON object"},
		{name: "response/redirect is not followed", args: search, api: redirectTo("http://127.0.0.1:1/steal"), exit: 1, code: "api_error", status: 302},
		{name: "response/connection closed", args: search, api: closeConnection, exit: 1, code: "network_error", msg: "Request to /search.json failed"},
		{name: "response/no answer in time", args: []string{"account", "--timeout", "50ms"}, api: hang, exit: 1, code: "network_error",
			msg: "Request to /account.json timed out after 50ms"},
		{name: "response/SERPAPI_TIMEOUT applies", args: []string{"account"}, env: map[string]string{"SERPAPI_TIMEOUT": "0.05"}, api: hang, exit: 1,
			code: "network_error", msg: "Request to /account.json timed out after 50ms. Slow engines, such as google_ai_mode"},

		{name: "paging/foreign next host and key", args: searchWith("--all-pages"), api: func(w http.ResponseWriter, r *http.Request) {
			if pageNumber(r) == 1 {
				fmt.Fprint(w, `{"organic_results":[1],"serpapi_pagination":{"next":"https://evil.example/search.json?engine=google&q=coffee&start=10&api_key=stolen"}}`)
				return
			}
			fmt.Fprint(w, `{"organic_results":[2]}`)
		}, reqs: 2, stdout: "{\n  \"organic_results\": [\n    1,\n    2\n  ]\n}\n", query: map[string]string{"api_key": "envkey"}},
		{name: "paging/invalid next URL", args: searchWith("--all-pages"), api: respond(`{"organic_results":[1],"serpapi_pagination":{"next":"http://[::1"}}`),
			exit: 1, code: "api_error", msg: "Invalid pagination URL in response: http://[::1 (page 2 failed; page 1 was printed)", stdout: "{\n  \"organic_results\": [\n    1\n  ]\n}\n"},
		{name: "paging/jq runs on the merged pages", args: searchWith("--all-pages", "--jq", ".organic_results | length"), api: pages, reqs: 3, stdout: "3\n"},

		{name: "output/small result is quiet", args: search, api: oneResult, quiet: true},
		{name: "output/large result names its keys and the archive", args: search, api: respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)),
			warn: "keys by size: organic_results 52 KB, search_metadata 24 B. To print only what you need, read the stored search for free: serpapi archive abc123 --jq '.KEY'"},
		{name: "output/large result without an id", args: search, api: respond(bigResult("", 1500)),
			warn: "repeat the command with --jq '.KEY': the same search within an hour comes from the cache"},
		{name: "output/large no_cache result promises no cache hit", args: searchWith("no_cache=true"), api: respond(bigResult("", 1500)),
			warn: "repeat the command with --jq '.KEY'; no_cache=true makes the repeat a new, counted search"},
		{name: "output/large no_cache result keeps the free archive", args: searchWith("no_cache=true"), api: respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)),
			warn: "read the stored search for free: serpapi archive abc123 --jq '.KEY'"},
		{name: "output/large no_cache in the API's true words", args: searchWith("no_cache=YES"), api: respond(bigResult("", 1500)),
			warn: "no_cache=YES makes the repeat a new, counted search"},
		{name: "output/large no_cache in the API's false words", args: searchWith("no_cache=False"), api: respond(bigResult("", 1500)),
			warn: "repeat the command with --jq '.KEY': the same search within an hour comes from the cache"},
		{name: "output/large no_cache in neither list", args: searchWith("no_cache=maybe"), api: respond(bigResult("", 1500)),
			warn: "repeat the command with --jq '.KEY'; with no_cache=maybe the repeat may be a new, counted search"},
		{name: "output/large zero_trace result has no archive", args: searchWith("zero_trace=true"), api: respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)),
			warn: "repeat the command with --jq '.KEY'; zero_trace=true stores nothing and makes the repeat a new, counted search"},
		{name: "output/large zero_trace=0 keeps the archive", args: searchWith("zero_trace=0"), api: respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)),
			warn: "read the stored search for free: serpapi archive abc123 --jq '.KEY'"},
		{name: "output/large result with --jq is quiet", args: searchWith("--jq", ".organic_results | length"),
			api: respond(bigResult("", 1500)), stdout: "1500\n", quiet: true},
		{name: "output/large result with --fields still hints", args: searchWith("--fields", "organic_results"),
			api: respond(bigResult("", 1500)), warn: "Large result: 52 KB"},
		{name: "output/large merged pages", args: searchWith("--all-pages"), reqs: 2, api: bigPages,
			warn: "Large result: 48 KB; top-level keys by size: organic_results 48 KB, inline_images 70 B. To print only what you need, repeat the command"},
		{name: "output/large no_cache merged pages", args: searchWith("no_cache=true", "--all-pages"), reqs: 2, api: bigPages,
			warn: "no_cache=true makes the repeat a new, counted search for each page"},
		{name: "output/large zero_trace merged pages", args: searchWith("zero_trace=1", "--all-pages"), reqs: 2, api: bigPages,
			warn: "zero_trace=1 stores nothing and makes the repeat a new, counted search for each page"},
		{name: "output/large archive", args: []string{"archive", "abc123"}, api: respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)),
			warn: "add --jq '.KEY'; reading the archive is free"},
		{name: "output/jq printing only null lists the keys", args: searchWith("--jq", ".organic"), api: oneResult, stdout: "\n",
			warn: "--jq printed only null; check the path against the response's top-level keys: organic_results"},
		{name: "output/jq printing some values is quiet", args: searchWith("--jq", ".organic_results[0].position, .organic"), api: oneResult,
			stdout: "1\n\n", quiet: true},
		{name: "output/jq printing nothing is quiet", args: searchWith("--jq", ".organic_results[] | select(.position > 5)"), api: oneResult,
			stdout: "", quiet: true},
		{name: "output/jq streams results before a runtime error", args: searchWith("--jq", `.a[] | . + 1`), api: respond(`{"a":[1,2,"x"]}`),
			exit: 2, code: "usage_error", msg: "cannot add", stdout: "2\n3\n"},
	}
}

func TestEdgeCases(t *testing.T) {
	for _, tc := range edgeCases() {
		t.Run(tc.name, func(t *testing.T) {
			reqs := fakeAPI(t, tc.api)
			t.Setenv("SERPAPI_KEY", "envkey")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if tc.setup != nil {
				tc.setup(t)
			}

			res := execute(t, "", tc.args...)

			if tc.exit == 0 {
				warnings := wantOK(t, res)
				if tc.warn != "" && !strings.Contains(strings.Join(warnings, "\n"), tc.warn) {
					t.Errorf("warnings = %q, want one containing %q", warnings, tc.warn)
				}
				if tc.quiet && len(warnings) > 0 {
					t.Errorf("warnings = %q, want none", warnings)
				}
			} else {
				errObj := wantError(t, res, tc.exit, tc.code, tc.msg)
				if tc.exact && errObj["message"] != tc.msg {
					t.Errorf("message = %q, want exactly %q", errObj["message"], tc.msg)
				}
				if got, _ := errObj["status"].(float64); int(got) != tc.status {
					t.Errorf("status = %v, want %d", errObj["status"], tc.status)
				}
			}
			if (tc.stdout != "" || tc.exit != 0) && res.stdout != tc.stdout {
				t.Errorf("stdout = %q, want %q", res.stdout, tc.stdout)
			}
			if strings.Contains(res.stdout+res.stderr, strayKey) {
				t.Errorf("output leaks the key: %s%s", res.stdout, res.stderr)
			}
			if tc.api == nil {
				return
			}
			want := tc.reqs
			if want == 0 {
				want = 1
			}
			urls := reqs.list()
			if len(urls) != want {
				t.Fatalf("got %d requests, want %d", len(urls), want)
			}
			for _, u := range urls {
				q := u.Query()
				for k, v := range tc.query {
					if !q.Has(k) || q.Get(k) != v {
						t.Errorf("%s: %s = %q, want %q", u.Path, k, q.Get(k), v)
					}
				}
				for _, k := range tc.noQuery {
					if q.Has(k) {
						t.Errorf("%s sent %s", u.Path, k)
					}
				}
			}
		})
	}
}

// bigResult is a response of n organic results with only a position each,
// after the given top-level members.
func bigResult(members string, n int) string {
	var b strings.Builder
	b.WriteString("{" + members + `"organic_results":[`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"position":%d}`, i)
	}
	b.WriteString("]}")
	return b.String()
}

func writeConfig(content string) func(t *testing.T) {
	return func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(config.Path()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.Path(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func unreadableConfig(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions do not stop this user from reading")
	}
	writeConfig(`api_key = "savedkey"`)(t)
	if err := os.Chmod(config.Path(), 0); err != nil {
		t.Fatal(err)
	}
}

func redirectTo(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, location, http.StatusFound) }
}

func closeConnection(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
	}
}

func hang(_ http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(5 * time.Second):
	}
}

// failingWriter fails every write after the first limit bytes, like a pipe
// whose reader went away.
type failingWriter struct{ limit int }

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.limit -= len(p); f.limit < 0 {
		return 0, fmt.Errorf("broken pipe")
	}
	return len(p), nil
}

func TestEndlessJQOutputStopsWhenStdoutFails(t *testing.T) {
	fakeAPI(t, oneResult)
	t.Setenv("SERPAPI_KEY", "envkey")
	resetFlags(rootCmd)
	rootCmd.SetOut(&failingWriter{limit: 1 << 16})
	var stderr strings.Builder
	rootCmd.SetErr(&stderr)
	rootCmd.SetIn(strings.NewReader(""))
	rootCmd.SetArgs(searchWith("--jq", "repeat(1)"))

	done := make(chan int, 1)
	go func() { done <- run(context.Background()) }()
	select {
	case code := <-done:
		_, errObj := parseStderr(t, stderr.String())
		if code != 1 || errObj["code"] != "api_error" || !strings.Contains(errObj["message"].(string), "broken pipe") {
			t.Errorf("exit %d, error %v; want api_error about the broken pipe", code, errObj)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("endless jq output kept running after stdout failed")
	}
}

func TestInterruptDuringAllPagesPrintsFetchedPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := make(chan struct{})
	reqs := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if n := pageNumber(r); n < 3 {
			fmt.Fprint(w, page(n, true))
			return
		}
		close(reached)
		<-r.Context().Done()
	})
	t.Setenv("SERPAPI_KEY", "envkey")
	go func() {
		<-reached
		cancel()
	}()

	res := executeContext(t, ctx, "", searchWith("--all-pages")...)

	wantError(t, res, 130, "interrupted", "Stopped while fetching page 3; pages 1-2 were printed")
	if got := len(organicResults(t, res.stdout)); got != 2 {
		t.Errorf("printed %d results, want the 2 fetched pages", got)
	}
	if got := len(reqs.list()); got != 3 {
		t.Errorf("got %d requests, want 3", got)
	}
}

// interruptAtPage3 serves pages 1 and 2 and cancels ctx once page 3 is asked
// for, as Ctrl-C during a slow page would.
func interruptAtPage3(t *testing.T, cancel context.CancelFunc) {
	t.Helper()
	reached := make(chan struct{})
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if n := pageNumber(r); n < 3 {
			fmt.Fprint(w, page(n, true))
			return
		}
		close(reached)
		<-r.Context().Done()
	})
	t.Setenv("SERPAPI_KEY", "envkey")
	go func() {
		<-reached
		cancel()
	}()
}

// The pages fetched before Ctrl-C were billed, so --jq still runs on them.
func TestInterruptDuringAllPagesWithJQPrintsFetchedPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interruptAtPage3(t, cancel)

	res := executeContext(t, ctx, "", searchWith("--all-pages", "--jq", ".organic_results | length")...)

	wantError(t, res, 130, "interrupted", "Stopped while fetching page 3; pages 1-2 were printed")
	if res.stdout != "2\n" {
		t.Errorf("stdout = %q, want --jq's result on the 2 fetched pages", res.stdout)
	}
}

// --jq on the fetched pages gets interruptGrace after the signal, no more.
func TestInterruptDuringAllPagesBoundsAnEndlessJQ(t *testing.T) {
	grace := interruptGrace
	interruptGrace = 100 * time.Millisecond
	t.Cleanup(func() { interruptGrace = grace })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interruptAtPage3(t, cancel)

	start := time.Now()
	res := executeContext(t, ctx, "", searchWith("--all-pages", "--jq", "reduce range(1e12) as $i (0; . + 1)")...)

	wantError(t, res, 130, "interrupted", "Stopped while running --jq")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v, want --jq stopped about interruptGrace after the signal", elapsed)
	}
}

func TestInterruptBeforeTheResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeAPI(t, func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := executeContext(t, ctx, "", search...)

	errObj := wantError(t, res, 130, "interrupted", "Stopped before the response arrived")
	if _, ok := errObj["status"]; ok || res.stdout != "" {
		t.Errorf("error %v, stdout %q; want no status and no output", errObj, res.stdout)
	}
}

func TestInterruptDuringJQKeepsWhatItPrinted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"organic_results":[{"position":1}]}`)
		time.AfterFunc(50*time.Millisecond, cancel)
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := executeContext(t, ctx, "", searchWith("--jq", ".organic_results[0].position, (reduce range(1e12) as $i (0; . + 1))")...)

	wantError(t, res, 130, "interrupted", "Stopped while running --jq")
	if res.stdout != "1\n" {
		t.Errorf("stdout = %q, want the result printed before the signal", res.stdout)
	}
}

// cancelAtEndOf cancels ctx as the client reads the last byte of page n:
// after the page arrived, before anything is printed.
func cancelAtEndOf(t *testing.T, n int, cancel context.CancelFunc) {
	t.Helper()
	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := orig.RoundTrip(r)
		if err == nil && pageNumber(r) == n {
			resp.Body = &onEOF{ReadCloser: resp.Body, do: cancel}
		}
		return resp, err
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// onEOF calls do once the body has been read to the end.
type onEOF struct {
	io.ReadCloser
	do func()
}

func (b *onEOF) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.do()
	}
	return n, err
}

// A signal after the last page arrived, before it was printed, still prints
// every page: all of them were billed.
func TestInterruptAfterTheLastPagePrintsThemAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page(pageNumber(r), pageNumber(r) < 3)) })
	t.Setenv("SERPAPI_KEY", "envkey")
	cancelAtEndOf(t, 3, cancel)

	res := executeContext(t, ctx, "", searchWith("--all-pages", "--jq", ".organic_results | length")...)

	wantOK(t, res)
	if res.stdout != "3\n" || ctx.Err() == nil {
		t.Errorf("stdout = %q, signaled = %v; want --jq's result on all 3 pages after the signal", res.stdout, ctx.Err() != nil)
	}
}

func TestInterruptAfterTheResponseStillRunsJQ(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeAPI(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, page(1, false)) })
	t.Setenv("SERPAPI_KEY", "envkey")
	cancelAtEndOf(t, 1, cancel)

	res := executeContext(t, ctx, "", searchWith("--jq", ".organic_results[0].position")...)

	wantOK(t, res)
	if res.stdout != "1\n" || ctx.Err() == nil {
		t.Errorf("stdout = %q, signaled = %v; want --jq's result after the signal", res.stdout, ctx.Err() != nil)
	}
}

// --jq that fails on the pages fetched before a signal reports its own error:
// those pages did arrive.
func TestJQErrorAfterAnInterruptIsReported(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interruptAtPage3(t, cancel)

	res := executeContext(t, ctx, "", searchWith("--all-pages", "--jq", ".organic_results[].position, (.missing | keys)")...)

	wantError(t, res, 2, "usage_error", "keys cannot be applied to: null")
	if res.stdout != "1\n2\n" {
		t.Errorf("stdout = %q, want what --jq printed before its error", res.stdout)
	}
}

// After a signal, --jq on the fetched pages stops, and flushes what it
// printed, before the process would exit at interruptGrace.
func TestInterruptedJQStopsBeforeTheProcessExits(t *testing.T) {
	grace := interruptGrace
	interruptGrace = time.Second
	t.Cleanup(func() { interruptGrace = grace })
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	interruptAtPage3(t, func() { cancel(&signaled{at: time.Now()}) })
	var watchdog lockedBuffer
	done, returned := make(chan struct{}), make(chan struct{})
	go func() {
		exitAfterGrace(ctx, done, func() {}, &watchdog, func(int) {})
		close(returned)
	}()

	res := executeContext(t, ctx, "", searchWith("--all-pages", "--jq", ".organic_results[0].position, (reduce range(1e12) as $i (0; . + 1))")...)
	close(done)
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("exitAfterGrace never returned")
	}

	if got := watchdog.String(); got != "" {
		t.Errorf("the process would have exited before --jq stopped: %s", got)
	}
	wantError(t, res, 130, "interrupted", "Stopped while running --jq")
	if res.stdout != "1\n" {
		t.Errorf("stdout = %q, want the value --jq printed before it was stopped", res.stdout)
	}
}

// lockedBuffer is a bytes.Buffer safe to write from another goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestExitAfterGrace(t *testing.T) {
	grace := interruptGrace
	interruptGrace = 20 * time.Millisecond
	t.Cleanup(func() { interruptGrace = grace })

	t.Run("a command stuck after the signal exits 130", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stderr lockedBuffer
		exited, stopped := make(chan int, 1), false
		exitAfterGrace(ctx, make(chan struct{}), func() { stopped = true }, &stderr, func(code int) { exited <- code })

		if code := <-exited; code != 130 || !stopped {
			t.Errorf("exit(%d), stopped %v; want exit(130) after restoring the default handling", code, stopped)
		}
		if !strings.Contains(stderr.String(), `"code":"interrupted","message":"Stopped 20ms after the signal; the output may be cut off"`) {
			t.Errorf("stderr = %q", stderr.String())
		}
	})
	t.Run("a command that finishes in time is not cut off", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		close(done)
		exitAfterGrace(ctx, done, func() {}, io.Discard, func(code int) { t.Errorf("exit(%d) after the command finished", code) })
	})
	t.Run("no signal, no exit", func(t *testing.T) {
		done := make(chan struct{})
		close(done)
		exitAfterGrace(context.Background(), done, func() { t.Error("stop called without a signal") }, io.Discard,
			func(code int) { t.Errorf("exit(%d) without a signal", code) })
	})
}

// The process exits interruptGrace after the signal, however long the
// command took to notice it.
func TestExitAfterGraceCountsFromTheSignal(t *testing.T) {
	grace := interruptGrace
	interruptGrace = 2 * time.Second
	t.Cleanup(func() { interruptGrace = grace })
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&signaled{at: time.Now().Add(-interruptGrace)})
	exited := make(chan struct{})
	start := time.Now()

	go exitAfterGrace(ctx, make(chan struct{}), func() {}, io.Discard, func(int) { close(exited) })

	select {
	case <-exited:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("exited %v after the watchdog woke, want at once: the grace had run out", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never exited")
	}
}

// --jq after a signal stops a quarter of interruptGrace before the process
// exits, counted from the signal, so a slow decode before printing doesn't
// push it past the exit.
func TestPrintDeadlineCountsFromTheSignal(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	at := time.Now().Add(-time.Second)
	cancel(&signaled{at: at})

	printCtx, stop := printContext(ctx)
	defer stop()

	want := at.Add(interruptGrace - interruptGrace/4)
	if d, ok := printCtx.Deadline(); !ok || !d.Equal(want) {
		t.Errorf("deadline %v (set: %v), want %v", d, ok, want)
	}
}

// net/http reports a canceled request with the context's cause, so a signal
// still reads as a stop before the response.
func TestSignalBeforeTheResponse(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	fakeAPI(t, func(_ http.ResponseWriter, r *http.Request) {
		cancel(&signaled{at: time.Now()})
		<-r.Context().Done()
	})
	t.Setenv("SERPAPI_KEY", "envkey")

	res := executeContext(t, ctx, "", search...)

	wantError(t, res, 130, "interrupted", "Stopped before the response arrived")
}

package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const extractKey = "extract_test_key"

func TestExtractSendsTheURLAsAnExtractSearch(t *testing.T) {
	const page = "https://example.com/a?x=1&y=2"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"bare URL", []string{page}},
		{"url=", []string{"url=" + page}},
		{"bare URL after params", []string{"mode=map", "include_sitemap=false", page}},
		{"engine=extract given", []string{"engine=extract", page}},
		{"engine in another case", []string{"engine=Extract", page}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqs := fakeAPI(t, respond(`{"search_metadata":{"id":"e1"},"page_information":{"title":"Example Domain"}}`))

			res := execute(t, "", append([]string{"--api-key", extractKey, "extract"}, tc.args...)...)

			wantWarnings(t, wantOK(t, res))
			if !strings.Contains(res.stdout, `"title": "Example Domain"`) {
				t.Errorf("stdout = %q, want the indented response", res.stdout)
			}
			u := reqs.only(t)
			wantQuery(t, u, "/search.json", map[string]string{"engine": "extract", "url": page, "api_key": extractKey})
			if q := u.Query(); q.Has("q") || q.Has("x") || q.Has("y") {
				t.Errorf("query = %s, want the URL's own query kept inside url=", u.RawQuery)
			}
		})
	}
}

func TestExtractSendsParamsAndFields(t *testing.T) {
	reqs := fakeAPI(t, respond(`{"pages":[{"url":"https://example.com/"}]}`))

	res := execute(t, "", "--api-key", extractKey, "extract", "https://example.com/", "mode=crawl", "max_pages=5",
		"--fields", "pages[].url", "--jq", ".pages[0].url")

	wantOK(t, res)
	if got := strings.TrimSpace(res.stdout); got != "https://example.com/" {
		t.Errorf("stdout = %q, want the --jq result", got)
	}
	wantQuery(t, reqs.only(t), "/search.json", map[string]string{
		"engine": "extract", "mode": "crawl", "max_pages": "5", "json_restrictor": "pages[].url,error",
	})
}

func TestExtractPrintsMarkdownVerbatim(t *testing.T) {
	reqs := fakeAPI(t, respond("---\nsearch_parameters:\n  engine: extract\n---\n\nThis domain is for use in documentation examples.\n"))

	res := execute(t, "", "--api-key", extractKey, "extract", "https://example.com/", "output=md")

	wantOK(t, res)
	if res.stdout != "---\nsearch_parameters:\n  engine: extract\n---\n\nThis domain is for use in documentation examples.\n" {
		t.Errorf("stdout = %q, want the Markdown as sent", res.stdout)
	}
	wantQuery(t, reqs.only(t), "/search.json", map[string]string{"engine": "extract", "output": "md"})
}

func TestExtractReportsAPIErrors(t *testing.T) {
	fakeAPI(t, respond(`{"search_metadata":{"status":"Error"},"error":"URL is disallowed by robots.txt."}`))

	res := execute(t, "", "--api-key", extractKey, "extract", "https://example.com/private")

	if status := wantError(t, res, 1, "api_error", "URL is disallowed by robots.txt.")["status"]; status != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", status)
	}
}

// Each of these is wrong before any request, so none may cost a search.
func TestExtractUsageErrorsAreUnsent(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{"no URL", nil, "No URL given, e.g. serpapi extract https://example.com"},
		{"params but no URL", []string{"mode=map"}, "No URL given"},
		{"two URLs", []string{"https://a.example", "https://b.example"}, "extract takes one URL, got 2: https://a.example, https://b.example"},
		{"URL and url=", []string{"https://a.example", "url=https://b.example"}, "extract takes one URL, got 2"},
		{"two url=", []string{"url=https://a.example", "url=https://b.example"}, "extract takes one URL, got 2: https://a.example, https://b.example"},
		{"parameter without =", []string{"https://example.com", "main"},
			`Unexpected argument "main": API parameters are key=value pairs, e.g. include_tags=main`},
		{"no scheme", []string{"example.com/page"}, `Invalid URL "example.com/page": the Extract API takes an absolute http or https URL, e.g. https://example.com/page`},
		{"other scheme", []string{"ftp://example.com/file"}, "takes an absolute http or https URL"},
		{"relative path", []string{"/about"}, `Invalid URL "/about"`},
		{"empty url=", []string{"url="}, `Invalid URL ""`},
		{"other engine", []string{"https://example.com", "engine=google"}, "for engine=google, use 'serpapi search engine=google'"},
		{"key as a parameter", []string{"https://example.com", "api_key=secret"}, "Pass the API key with --api-key"},
		{"jq with Markdown", []string{"https://example.com", "output=md", "--jq", ".x"}, "--jq needs JSON, but output=md is not JSON"},
		{"unknown output", []string{"https://example.com", "output=xml"}, "Unknown output=xml"},
		{"parameter as a flag", []string{"https://example.com", "--mode", "map"}, "unknown flag: --mode; API parameters are key=value pairs, e.g. mode=VALUE"},
		{"one-letter flag", []string{"https://example.com", "-m", "map"}, "API parameters are key=value pairs, e.g. include_tags=main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAPI(t, nil)
			res := execute(t, "", append([]string{"--api-key", extractKey, "extract"}, tc.args...)...)
			wantError(t, res, 2, "usage_error", tc.msg)
			if strings.Contains(res.stderr, "given more than once") {
				t.Errorf("stderr = %q, want the error alone, not a warning that the last value is used", res.stderr)
			}
		})
	}
}

// Unlike search, the Extract API answers nothing without a key, so the CLI
// stops before sending.
func TestExtractNeedsAKeyBeforeSending(t *testing.T) {
	fakeAPI(t, nil)

	res := execute(t, "", "extract", "https://example.com")

	wantError(t, res, 2, "usage_error", "No API key")
}

// map and crawl are extract with mode= preset.
func TestMapAndCrawlSendTheirMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want map[string]string
	}{
		{"map", []string{"map", "https://example.com", "include_sitemap=false"},
			map[string]string{"mode": "map", "include_sitemap": "false"}},
		{"crawl", []string{"crawl", "url=https://example.com", "max_pages=5", "max_depth=1"},
			map[string]string{"mode": "crawl", "max_pages": "5", "max_depth": "1"}},
		{"same mode given", []string{"crawl", "https://example.com", "mode=crawl"}, map[string]string{"mode": "crawl"}},
		{"same mode in another case", []string{"map", "https://example.com", "mode=MAP"}, map[string]string{"mode": "map"}},
		{"crawl with fields", []string{"crawl", "https://example.com", "--fields", "pages[].url"},
			map[string]string{"mode": "crawl", "json_restrictor": "pages[].url,error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqs := fakeAPI(t, respond(`{"search_metadata":{"id":"e1"}}`))

			res := execute(t, "", append([]string{"--api-key", extractKey}, tc.args...)...)

			wantWarnings(t, wantOK(t, res))
			u := reqs.only(t)
			wantQuery(t, u, "/search.json", map[string]string{"engine": "extract", "url": "https://example.com", "api_key": extractKey})
			wantQuery(t, u, "/search.json", tc.want)
		})
	}
}

func TestMapAndCrawlUsageErrorsAreUnsent(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{"crawl with mode=map", []string{"crawl", "https://example.com", "mode=map"},
			"crawl always uses mode=crawl; drop mode=map, or use 'serpapi extract URL mode=map'"},
		{"map with mode=scrape", []string{"map", "https://example.com", "mode=scrape"}, "map always uses mode=map; drop mode=scrape"},
		{"map without a URL", []string{"map"}, "No URL given, e.g. serpapi map https://example.com"},
		{"map with two URLs", []string{"map", "https://a.example", "https://b.example"},
			"map takes one URL, got 2: https://a.example, https://b.example; to extract several pages of one website, use 'serpapi crawl URL'"},
		{"crawl without a scheme", []string{"crawl", "example.com"}, "e.g. https://example.com"},
		{"search's --max-pages on crawl", []string{"crawl", "https://example.com", "--max-pages", "5"},
			"unknown flag: --max-pages; API parameters are key=value pairs, e.g. max_pages=VALUE"},
		{"search's --all-pages on crawl", []string{"crawl", "https://example.com", "--all-pages"},
			"unknown flag: --all-pages; --all-pages works only with search; API parameters are key=value pairs, e.g. max_pages=5 max_depth=1"},
		{"search's --image on map", []string{"map", "https://example.com", "--image", "photo.png"},
			"unknown flag: --image; --image works only with search; API parameters are key=value pairs, e.g. include_sitemap=false"},
		{"crawl parameter without =", []string{"crawl", "https://example.com", "max_pages", "5"},
			`Unexpected argument "max_pages": API parameters are key=value pairs, e.g. max_pages=5 max_depth=1`},
		{"one-letter flag on map", []string{"map", "https://example.com", "-s"}, "e.g. include_sitemap=false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAPI(t, nil)
			res := execute(t, "", append([]string{"--api-key", extractKey}, tc.args...)...)
			wantError(t, res, 2, "usage_error", tc.msg)
		})
	}
}

// A crawl given two URLs is not told to use crawl.
func TestCrawlWithTwoURLsGetsNoCrawlAdvice(t *testing.T) {
	fakeAPI(t, nil)

	res := execute(t, "", "--api-key", extractKey, "crawl", "https://a.example", "https://b.example")

	if msg, _ := wantError(t, res, 2, "usage_error", "crawl takes one URL, got 2")["message"].(string); strings.Contains(msg, "serpapi crawl") {
		t.Errorf("message = %q, want no advice to use crawl", msg)
	}
}

func TestMapAndCrawlNeedAKeyBeforeSending(t *testing.T) {
	for _, name := range []string{"map", "crawl"} {
		t.Run(name, func(t *testing.T) {
			fakeAPI(t, nil)
			wantError(t, execute(t, "", name, "https://example.com"), 2, "usage_error", "No API key")
		})
	}
}

// --debug and --timeout are global flags; extract, map and crawl must honor
// them as search does: a JSON trace on piped stderr, without the key, and the
// timeout in effect.
func TestExtractCommandsTraceWithDebug(t *testing.T) {
	for _, name := range []string{"extract", "map", "crawl"} {
		t.Run(name, func(t *testing.T) {
			fakeAPI(t, respond(debugSearchBody))

			res := execute(t, "", "--api-key", extractKey, "--debug", "--timeout", "120", name, "https://example.com/")

			var events []string
			for _, line := range strings.Split(strings.TrimSpace(res.stderr), "\n") {
				var v struct {
					Debug *struct {
						Event string `json:"event"`
					} `json:"debug"`
				}
				if err := json.Unmarshal([]byte(line), &v); err != nil || v.Debug == nil {
					t.Fatalf("stderr line is not a JSON debug object: %q", line)
				}
				events = append(events, v.Debug.Event)
			}
			if res.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr: %s", res.code, res.stderr)
			}
			joined := strings.Join(events, "\n")
			for _, want := range []string{"GET http", "api_key=[REDACTED]", "engine=extract", "Timeout: 120s", "Server search: id=abc123", "Server timeline:"} {
				if !strings.Contains(joined, want) {
					t.Errorf("trace missing %q:\n%s", want, joined)
				}
			}
			if strings.Contains(res.stderr, extractKey) {
				t.Error("trace leaked the API key")
			}
			if !strings.Contains(res.stdout, `"abc123"`) {
				t.Errorf("stdout = %q, want the result untouched by the trace", res.stdout)
			}
		})
	}
}

func TestExtractCommandsTimeOut(t *testing.T) {
	const extraction = "Request to /search.json timed out after 50ms: retry with a higher --timeout, such as --timeout 120"
	const crawl = "Request to /search.json timed out after 50ms. A crawl extracts every page it visits, so it takes longer than one page: " +
		"retry with a higher --timeout, such as --timeout 300"
	for _, tc := range []struct {
		name, env, want string
		args            []string
	}{
		{"extract --timeout", "", extraction, []string{"--timeout", "0.05", "extract"}},
		{"map --timeout", "", extraction, []string{"--timeout", "50ms", "map"}},
		{"extract mode=crawl", "", crawl, []string{"--timeout", "50ms", "extract", "mode=crawl"}},
		{"crawl --timeout", "", crawl, []string{"--timeout", "0.05", "crawl"}},
		{"crawl SERPAPI_TIMEOUT", "0.05", crawl, []string{"crawl"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAPI(t, hang)
			t.Setenv("SERPAPI_TIMEOUT", tc.env)

			start := time.Now()
			res := execute(t, "", append(append([]string{"--api-key", extractKey}, tc.args...), "https://example.com/")...)

			wantError(t, res, 1, "network_error", tc.want)
			if strings.Contains(res.stdout+res.stderr, "Slow engines") {
				t.Errorf("advice blames slow engines:\n%s%s", res.stdout, res.stderr)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("took %v, want the 50ms timeout to stop it", elapsed)
			}
		})
	}
}

func TestIsURLArg(t *testing.T) {
	for arg, want := range map[string]bool{
		"https://example.com/?a=1&b=2": true,
		"example.com":                  true,
		"example.com?a=1":              true,
		"example.com/path?a=1":         true,
		"":                             true,
		"max_pages":                    false,
		"localhost":                    false,
		"mode=map":                     false,
		"url=https://example.com/?a=1": false,
		"include_tags=article, main":   false,
		"exclude_tags=.ads,#cookie":    false,
	} {
		if got := isURLArg(arg); got != want {
			t.Errorf("isURLArg(%q) = %v, want %v", arg, got, want)
		}
	}
}

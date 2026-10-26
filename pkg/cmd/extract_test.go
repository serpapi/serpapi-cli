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
		{"empty URL with a URL", []string{"", "https://example.com"}, `Invalid URL "": the Extract API takes an absolute http or https URL`},
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
		{"search's --max-pages", []string{"https://example.com", "--max-pages", "3"},
			"unknown flag: --max-pages; --max-pages works only with search; API parameters are key=value pairs, e.g. include_tags=main"},
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
		{"search's --max-pages on map", []string{"map", "https://example.com", "--max-pages", "5"},
			"unknown flag: --max-pages; --max-pages works only with search; API parameters are key=value pairs, e.g. include_sitemap=false"},
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

// --debug is a global flag; extract must honor it as search does: a JSON
// trace on piped stderr, without the key, and stdout untouched.
func TestExtractTracesWithDebug(t *testing.T) {
	fakeAPI(t, respond(debugSearchBody))

	res := execute(t, "", "--api-key", extractKey, "--debug", "extract", "https://example.com/")

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
	for _, want := range []string{"GET http", "api_key=[REDACTED]", "engine=extract", "Server search: id=abc123", "Server timeline:"} {
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
}

// A crawl waits up to 300s unless --timeout or SERPAPI_TIMEOUT says
// otherwise; the trace shows the limit each request ran with.
func TestExtractTimeouts(t *testing.T) {
	for _, tc := range []struct {
		name, env, want string
		args            []string
	}{
		{"extract", "", "Timeout: 60s", []string{"extract"}},
		{"map", "", "Timeout: 60s", []string{"map"}},
		{"crawl", "", "Timeout: 300s", []string{"crawl"}},
		{"extract mode=crawl", "", "Timeout: 300s", []string{"extract", "mode=Crawl"}},
		{"crawl --timeout", "", "Timeout: 30s", []string{"--timeout", "30", "crawl"}},
		{"crawl --timeout 0", "", "Timeout: disabled", []string{"--timeout", "0", "crawl"}},
		{"crawl SERPAPI_TIMEOUT", "45", "Timeout: 45s", []string{"crawl"}},
		{"map --timeout", "", "Timeout: 120s", []string{"--timeout", "2m", "map"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAPI(t, respond(`{"search_metadata":{"id":"e1"}}`))
			t.Setenv("SERPAPI_TIMEOUT", tc.env)

			res := execute(t, "", append(append([]string{"--api-key", extractKey, "--debug"}, tc.args...), "https://example.com/")...)

			if res.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr: %s", res.code, res.stderr)
			}
			if !strings.Contains(res.stderr, `"event":"`+tc.want+`"`) {
				t.Errorf("trace has no %q:\n%s", tc.want, res.stderr)
			}
		})
	}
}

// The timeout stops the request; its advice is the Extract API's, worded in
// pkg/api, not search's slow engines.
func TestCrawlTimesOut(t *testing.T) {
	fakeAPI(t, hang)

	start := time.Now()
	res := execute(t, "", "--api-key", extractKey, "--timeout", "50ms", "crawl", "https://example.com/")

	wantError(t, res, 1, "network_error", "Request to /search.json timed out after 50ms. A crawl extracts every page it visits")
	if strings.Contains(res.stderr, "Slow engines") {
		t.Errorf("advice blames slow engines: %s", res.stderr)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v, want the 50ms timeout to stop it", elapsed)
	}
}

func TestCheckExtractURLSuggestsHTTPS(t *testing.T) {
	const takes = ": the Extract API takes an absolute http or https URL"
	for raw, want := range map[string]string{
		"example.com/page":      `Invalid URL "example.com/page"` + takes + ", e.g. https://example.com/page",
		"example.com:8080/page": `Invalid URL "example.com:8080/page"` + takes + ", e.g. https://example.com:8080/page",
		"mailto:me@x.com":       `Invalid URL "mailto:me@x.com"` + takes,
		"ftp://example.com":     `Invalid URL "ftp://example.com"` + takes,
		"":                      `Invalid URL ""` + takes,
	} {
		if err := checkExtractURL(raw); err == nil || err.Error() != "Usage error: "+want {
			t.Errorf("checkExtractURL(%q) = %v, want %s", raw, err, want)
		}
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

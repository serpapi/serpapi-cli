package cmd

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/params"
)

var extractCmd = &cobra.Command{
	Use:   "extract URL [PARAMS...]",
	Short: "Extract a web page as JSON or Markdown with the Extract API",
	Long: `Extract a public web page with the SerpApi Extract API (engine=extract):
https://serpapi.com/extract-api

The URL must be absolute http or https; quote it when it has ? or &. It may
also be given as url=URL. Other parameters are key=value pairs sent as-is:
  mode=scrape        the page itself (default)
  mode=map           the website's URLs; same as 'serpapi map'
  mode=crawl         every page of the website it visits; same as 'serpapi crawl'
  include_tags=, exclude_tags=   CSS selectors to keep or drop
  include_markdown=true          add the main content as Markdown to JSON
  output=md                      the main content as Markdown, after the
                                 search metadata
Search pages with their own engine, such as Google or Bing, are redirected to
it and return that engine's JSON. The Extract API runs no JavaScript and obeys
robots.txt, so a page that disallows it returns an error.

--fields and --jq work as in search, e.g. --jq .text_content.full. Entries
with a URL carry a serpapi_extract_link for extracting that URL next.

Cost: scrape and map count one search each; crawl counts one per page it
extracts. Cached and failed ones are free, as in search.`,
	Example: `  serpapi extract https://example.com
  serpapi extract https://example.com output=md
  serpapi extract https://example.com --jq ".inline_links[].link"
  serpapi extract https://example.com include_tags=main --jq .text_content.full`,
	Args: cobra.ArbitraryArgs,
	RunE: runExtractMode(""),
}

var mapCmd = &cobra.Command{
	Use:   "map URL [PARAMS...]",
	Short: "List a website's URLs with the Extract API (extract mode=map)",
	Long: `List a website's URLs, from the links on the page at URL and the website's
sitemaps. Same as 'serpapi extract URL mode=map'; see 'serpapi extract --help'.

Parameters are key=value pairs sent as-is:
  include_sitemap=false      only the page's links, not /sitemap.xml and the
                             sitemaps robots.txt lists
  include_subdomains=true    also other subdomains, such as blog.example.com
                             from www.example.com
URLs that robots.txt disallows are left out. Each entry in urls has a
serpapi_extract_link for extracting it next.

Cost: one search.`,
	Example: `  serpapi map https://books.toscrape.com
  serpapi map https://books.toscrape.com --jq ".urls[].url"
  serpapi map https://example.com include_sitemap=false include_subdomains=true`,
	Args: cobra.ArbitraryArgs,
	RunE: runExtractMode("map"),
}

var crawlCmd = &cobra.Command{
	Use:   "crawl URL [PARAMS...]",
	Short: "Extract a website's pages by following its links (extract mode=crawl)",
	Long: `Start at URL, follow links on the same website and extract every page
visited, as 'serpapi extract' does one page. Same as
'serpapi extract URL mode=crawl'; see 'serpapi extract --help'.

Parameters are key=value pairs sent as-is:
  max_pages=N                pages to extract, the first included (default 10,
                             at most 100)
  max_depth=N                links deep to follow; 0 is only the first page
                             (default 2, at most 3)
  include_subdomains=true    also follow links to other subdomains
Links that robots.txt disallows are not followed. The result has one entry
per page in pages.

Cost: one search per page extracted, the first included, so the default
max_pages=10 can cost 10; cached and failed pages are free.
search_metadata.credits_consumed says what it cost. Without --timeout or
SERPAPI_TIMEOUT, a crawl waits up to 300s, not 60s. timeout= is the Extract
API's own per-page fetch limit in milliseconds.`,
	Example: `  serpapi crawl https://books.toscrape.com max_pages=5 --jq ".pages[].url"
  serpapi crawl https://books.toscrape.com max_pages=20 max_depth=1
  serpapi crawl https://example.com/docs/ --fields "pages[].{url, text_content}"`,
	Args: cobra.ArbitraryArgs,
	RunE: runExtractMode("crawl"),
}

func init() {
	rootCmd.AddCommand(extractCmd, mapCmd, crawlCmd)
}

// isExtractCommand reports whether cmd sends an Extract API search.
func isExtractCommand(cmd *cobra.Command) bool {
	return cmd == extractCmd || cmd == mapCmd || cmd == crawlCmd
}

// runExtractMode returns the RunE of an Extract API command; a non-empty mode
// is sent as mode=, as map and crawl do.
func runExtractMode(mode string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error { return runExtract(cmd, args, mode) }
}

func runExtract(cmd *cobra.Command, args []string, mode string) error {
	p, err := extractParams(cmd, args, mode)
	if err != nil {
		return err
	}
	raw := false
	if o, ok := p["output"]; ok {
		if raw, err = checkOutput(o); err != nil {
			return err
		}
	}
	restricted := params.ApplyFields(p, fieldsFlag, "error") != ""

	// Unlike search, the Extract API answers nothing without a key.
	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}
	// A crawl extracts up to max_pages pages in one request, and one the CLI
	// gave up on still runs and is charged, so it gets longer by default.
	if strings.EqualFold(p["mode"], "crawl") && !timeoutSet {
		requestTimeout = api.DefaultCrawlTimeout
	}
	return sendSearch(cmd, newClient(apiKey), p, extractLabel(p["mode"]), raw, restricted)
}

// extractParams turns the arguments into the parameters of an Extract API
// search: the one URL, given bare or as url=, plus key=value pairs, and mode
// when the command sets one.
func extractParams(cmd *cobra.Command, args []string, mode string) (map[string]string, error) {
	var urls []string
	var parsed []params.Param
	for _, a := range args {
		if isURLArg(a) {
			urls = append(urls, a)
			continue
		}
		// search takes a bare word as q=; here it is a parameter missing its "=".
		if !strings.Contains(a, "=") {
			return nil, &clierrors.UsageError{Message: fmt.Sprintf("Unexpected argument %q: API parameters are key=value pairs, e.g. %s",
				a, extractParamExample(cmd))}
		}
		p, err := params.ParseParam(a)
		if err != nil {
			return nil, err
		}
		// Two url= are two URLs, refused below, not a repeat to warn about.
		if p.Key == "url" {
			urls = append(urls, p.Value)
			continue
		}
		parsed = append(parsed, p)
	}
	warnRepeated(cmd, parsed)
	p := params.ParamsToMap(parsed)

	name := cmd.Name()
	// An empty URL, as from an unset "$URL", would be a blank in the list below.
	if slices.Contains(urls, "") {
		return nil, checkExtractURL("")
	}
	switch len(urls) {
	case 0:
		return nil, &clierrors.UsageError{Message: "No URL given, e.g. serpapi " + name + " https://example.com"}
	case 1:
	default:
		msg := fmt.Sprintf("%s takes one URL, got %d: %s", name, len(urls), strings.Join(urls, ", "))
		if mode != "crawl" {
			msg += "; to extract several pages of one website, use 'serpapi crawl URL'"
		}
		return nil, &clierrors.UsageError{Message: msg}
	}
	if err := checkExtractURL(urls[0]); err != nil {
		return nil, err
	}
	if e, ok := p["engine"]; ok && !strings.EqualFold(e, "extract") {
		return nil, &clierrors.UsageError{Message: name + " always uses engine=extract; for engine=" + e + ", use 'serpapi search engine=" + e + "'"}
	}
	if m, ok := p["mode"]; ok && mode != "" && !strings.EqualFold(m, mode) {
		return nil, &clierrors.UsageError{Message: name + " always uses mode=" + mode + "; drop mode=" + m +
			", or use 'serpapi extract URL mode=" + m + "'"}
	}
	p["engine"] = "extract"
	p["url"] = urls[0]
	if mode != "" {
		p["mode"] = mode
	}
	return p, nil
}

// isURLArg reports whether arg is meant as the URL rather than a key=value
// parameter: a URL has ":", "/", "." or "?" before any "=" of its query, and
// no parameter name does. An empty argument, as from an unset "$URL", counts
// as the URL, so it is reported as an invalid one.
func isURLArg(arg string) bool {
	key, _, _ := strings.Cut(arg, "=")
	return arg == "" || strings.ContainsAny(key, ":/.?")
}

// extractParamExample is a parameter that cmd takes, for usage hints. It goes
// by name: comparing with mapCmd would make its initialization refer to itself.
func extractParamExample(cmd *cobra.Command) string {
	switch cmd.Name() {
	case "map":
		return "include_sitemap=false"
	case "crawl":
		return "max_pages=5 max_depth=1"
	}
	return "include_tags=main"
}

// checkExtractURL refuses, before any request, a URL the Extract API would:
// it takes only absolute http and https URLs.
func checkExtractURL(raw string) error {
	u, err := url.Parse(raw)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return nil
	}
	msg := fmt.Sprintf("Invalid URL %q: the Extract API takes an absolute http or https URL", raw)
	if !strings.Contains(raw, "://") {
		// Not mailto:me@x.com, which would parse as user "mailto:me" on x.com.
		if u, err := url.Parse("https://" + raw); err == nil && u.User == nil && strings.Contains(u.Hostname(), ".") {
			msg += ", e.g. https://" + raw
		}
	}
	return &clierrors.UsageError{Message: msg}
}

func extractLabel(mode string) string {
	switch strings.ToLower(mode) {
	case "map":
		return "Mapping website..."
	case "crawl":
		return "Crawling website..."
	}
	return "Extracting page..."
}

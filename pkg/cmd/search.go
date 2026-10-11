package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/output"
	"github.com/serpapi/serpapi-cli/pkg/params"
)

var (
	allPagesFlag bool
	maxPagesFlag int
)

const defaultMaxPages = 100

// carriedParams are dropped from serpapi_pagination.next but still apply to
// every page.
var carriedParams = []string{"json_restrictor", "no_cache", "zero_trace", "output"}

var searchCmd = &cobra.Command{
	Use:   "search PARAMS...",
	Short: "Run a search with any SerpApi engine",
	Long: `Run a search. Parameters are key=value pairs, starting with engine=.
Words without "=" are joined into q. Some engines name the query differently:
youtube search_query, amazon k, ebay _nkw, walmart and naver query, yahoo p,
yandex text. Parameters of every engine: https://serpapi.com/search-engine-apis

--fields (the json_restrictor parameter) makes the server return only the
listed paths: https://serpapi.com/json-restrictor
  organic_results[0:3]                  first three results
  organic_results[].{title, link}       two fields of every result
  search_metadata.id,local_results[0]   several paths, comma-separated
Unknown paths are dropped silently; if nothing matches you get {} and a warning.
The server also drops null, false and empty values. --fields is part of the
cache key, so a different --fields can miss the cache and count as a new
search; --jq filters the response locally without changing the request.

Responses are often 20-300 KB, and one key can be large too: Maps
place_results holds reviews and photos. To see the shape of an unfamiliar
response, print a sample with one item per array; nested arrays are cut too,
so it is not the answer:
  --jq 'walk(if type == "array" then .[:1] else . end)'
Then take what you need from the stored search, which is free:
  serpapi archive SEARCH_METADATA_ID --jq '.place_results | {title, rating, reviews}'

--all-pages follows serpapi_pagination.next and merges the arrays of every page
into one JSON object, up to --max-pages pages, one search each. It stops at a
page that --fields matches nothing on; use --jq to filter such pages instead.
If a later page fails, or Ctrl-C stops the run, the pages fetched so far are
printed and the error says how many.

async=true returns at once with search_metadata.id; collect the results later
with 'serpapi archive ID'.`,
	Example: `  serpapi search engine=google q=coffee
  serpapi search engine=google coffee shops --fields "organic_results[].{title, link}"
  serpapi search engine=youtube search_query="lofi beats" --jq ".video_results[:3][].link"
  serpapi search engine=google_maps q=pizza ll="@40.7455096,-74.0083012,14z" --fields "local_results[0:5]"
  serpapi search engine=google q=coffee --all-pages --max-pages 3 --jq ".organic_results | length"`,
	Args: cobra.ArbitraryArgs,
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&allPagesFlag, "all-pages", false, "follow pagination and merge the arrays of every page")
	searchCmd.Flags().IntVar(&maxPagesFlag, "max-pages", defaultMaxPages, "page limit for --all-pages; each page is one search")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return &clierrors.UsageError{Message: "No search parameters given, e.g. serpapi search engine=google q=coffee"}
	}
	parsed, err := params.ParseAll(args)
	if err != nil {
		return err
	}
	p := params.ParamsToMap(parsed)

	maxPagesSet := cmd.Flags().Changed("max-pages")
	if maxPagesSet && maxPagesFlag < 1 {
		return &clierrors.UsageError{Message: "--max-pages must be at least 1"}
	}
	if a := p["async"]; allPagesFlag && a != "" && a != "false" && a != "0" {
		return &clierrors.UsageError{Message: "--all-pages cannot follow an async search; drop async=" + a}
	}
	if maxPagesSet && !allPagesFlag {
		warn(cmd, "--max-pages has no effect without --all-pages")
	}

	keep := []string{"error"}
	if allPagesFlag {
		keep = append(keep, "serpapi_pagination.next")
	}
	restrictor := params.ApplyFields(p, fieldsFlag, keep...)

	// Search runs without a key, as in v0.2: the API answers a few demo
	// queries without one and returns 401 for the rest.
	apiKey, keyErr := resolveAPIKey()
	var cfgErr *clierrors.UsageError
	if keyErr != config.ErrNoAPIKey && errors.As(keyErr, &cfgErr) {
		warn(cmd, "%s; searching without a key", cfgErr.Message)
	}
	return sendSearch(cmd, newClient(apiKey), p, restrictor != "")
}

func sendSearch(cmd *cobra.Command, client *api.Client, p map[string]string, restricted bool) error {
	ctx := cmd.Context()
	if allPagesFlag {
		return searchAllPages(cmd, client, p, restricted)
	}

	result, err := withSpinner("Searching...", func() (json.RawMessage, error) { return client.Search(ctx, p) })
	if err != nil {
		return err
	}
	if restricted && isEmptyObject(result) {
		warnNothingMatched(cmd)
	}
	return handleOutput(cmd, result)
}

func isEmptyObject(raw []byte) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil && len(m) == 0
}

func warnNothingMatched(cmd *cobra.Command) {
	warn(cmd, "json_restrictor matched nothing or is malformed; compare its paths with the unfiltered response")
}

// searchAllPages fetches page after page and prints the merged result. Pages
// already fetched are printed even when a later page fails.
func searchAllPages(cmd *cobra.Command, client *api.Client, first map[string]string, restricted bool) error {
	carried := make(map[string]string)
	for _, k := range carriedParams {
		if v, ok := first[k]; ok {
			carried[k] = v
		}
	}

	ctx := cmd.Context()
	pageParams := first
	seen := map[string]bool{canonicalParamsKey(first): true}
	var merged map[string]any
	pages := 0
	var pageErr error
	for {
		raw, err := withSpinner(fmt.Sprintf("Fetching page %d...", pages+1), func() (json.RawMessage, error) {
			return client.Search(ctx, pageParams)
		})
		var page map[string]any
		if err == nil {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if decErr := dec.Decode(&page); decErr != nil {
				err = &clierrors.APIError{Message: "Failed to parse response: " + decErr.Error()}
			}
		}
		if err != nil {
			if pages == 0 {
				return err
			}
			pageErr = err
			break
		}
		pages++

		next := extractNextURL(page)
		if merged == nil {
			merged = page
		} else {
			mergeArrayFields(merged, page)
		}
		if next == "" {
			break
		}
		// Following next anyway would bill a search for every remaining page
		// of a mistyped restrictor.
		if restricted && !hasData(page) {
			if pages > 1 {
				warn(cmd, "Stopped after page %d: nothing on it matched json_restrictor; to fetch later pages anyway, filter with --jq instead of --fields", pages)
			}
			break
		}
		if pages >= maxPagesFlag {
			if !cmd.Flags().Changed("max-pages") {
				warn(cmd, "Stopped at %d pages, but more exist; raise --max-pages to fetch them (one search each)", pages)
			}
			break
		}
		nextParams, err := parseNextParams(next, carried)
		if err != nil {
			pageErr = err
			break
		}
		key := canonicalParamsKey(nextParams)
		if seen[key] {
			warn(cmd, "Stopped after page %d: the next page repeats an earlier one", pages)
			break
		}
		seen[key] = true
		pageParams = nextParams
	}

	if merged == nil {
		merged = map[string]any{}
	}
	delete(merged, "serpapi_pagination")
	if restricted && len(merged) == 0 {
		warnNothingMatched(cmd)
	}

	if err := printMerged(cmd, merged); err != nil {
		return err
	}
	if pageErr == nil {
		return nil
	}
	if ctx.Err() != nil {
		return &clierrors.InterruptedError{Message: fmt.Sprintf("Stopped while fetching page %d; %s printed", pages+1, printedPages(pages))}
	}

	// A 200 with an error body, such as "no results", usually means the
	// previous page was the last one.
	var apiErr *clierrors.APIError
	if errors.As(pageErr, &apiErr) && apiErr.Status == http.StatusOK {
		warn(cmd, "Stopped after page %d: %s", pages, apiErr.Message)
		return nil
	}
	return annotatePageError(pageErr, pages)
}

// printMerged prints the merged pages. --jq runs on them directly, and a
// pipe gets them encoded as they are written, so no encoded copy of every
// page is ever held in memory.
func printMerged(cmd *cobra.Command, merged map[string]any) error {
	if jqFlag != "" {
		return printJQ(cmd, merged)
	}
	w := cmd.OutOrStdout()
	if output.IsTerminal(w) {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(merged); err != nil {
			return &clierrors.APIError{Message: "Failed to encode result: " + err.Error()}
		}
		return outputError(output.PrintJSON(w, buf.Bytes()))
	}
	bw := bufio.NewWriterSize(w, 64<<10)
	if _, err := output.WriteObject(bw, merged, nil); err != nil {
		return outputError(err)
	}
	return outputError(bw.Flush())
}

func printedPages(pages int) string {
	if pages > 1 {
		return fmt.Sprintf("pages 1-%d were", pages)
	}
	return "page 1 was"
}

// annotatePageError adds which page failed and what was printed, keeping the
// error's type, status and cause.
func annotatePageError(err error, pages int) error {
	note := fmt.Sprintf(" (page %d failed; %s printed)", pages+1, printedPages(pages))

	var apiErr *clierrors.APIError
	var netErr *clierrors.NetworkError
	switch {
	case errors.As(err, &apiErr):
		return &clierrors.APIError{Message: apiErr.Message + note, Status: apiErr.Status, Cause: apiErr.Cause}
	case errors.As(err, &netErr):
		return &clierrors.NetworkError{Message: netErr.Message + note, Cause: netErr.Cause}
	}
	return fmt.Errorf("%w%s", err, note)
}

// hasData reports whether a page has anything besides pagination links.
func hasData(page map[string]any) bool {
	for k := range page {
		if k != "serpapi_pagination" {
			return true
		}
	}
	return false
}

// extractNextURL pulls the next pagination URL from a search result.
func extractNextURL(result map[string]any) string {
	pag, ok := result["serpapi_pagination"].(map[string]any)
	if !ok {
		return ""
	}
	next, _ := pag["next"].(string)
	return next
}

// mergeArrayFields appends array values from src into dst; other fields are overwritten.
func mergeArrayFields(dst, src map[string]any) {
	for key, val := range src {
		if newItems, ok := val.([]any); ok {
			if existing, ok := dst[key].([]any); ok {
				dst[key] = append(existing, newItems...)
				continue
			}
		}
		dst[key] = val
	}
}

// parseNextParams returns the query of a serpapi_pagination.next URL plus the
// carried parameters. The URL's host and path are ignored, so requests stay on
// the configured server and endpoint.
func parseNextParams(next string, carried map[string]string) (map[string]string, error) {
	u, err := url.Parse(next)
	if err != nil || u.RawQuery == "" {
		return nil, &clierrors.APIError{Message: "Invalid pagination URL in response: " + next}
	}
	result := make(map[string]string)
	for k, v := range u.Query() {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	for k, v := range carried {
		result[k] = v
	}
	return result, nil
}

// canonicalParamsKey produces a stable key from query params regardless of order.
func canonicalParamsKey(p map[string]string) string {
	vals := make(url.Values, len(p))
	for k, v := range p {
		vals.Set(k, v)
	}
	return vals.Encode()
}

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/config"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/params"
)

var (
	allPagesFlag bool
	maxPagesFlag int
)

const (
	defaultMaxPages          = 100
	paginationRestrictorPath = "serpapi_pagination.next"
)

var searchCmd = &cobra.Command{
	Use:   "search [PARAMS...]",
	Short: "Perform a search with any supported SerpApi engine",
	Example: `  serpapi search engine=google q=coffee
  serpapi search engine=google_light q="weather in Tokyo"
  serpapi search engine=google_maps q="pizza" ll="@40.7455096,-74.0083012,14z"
  serpapi search engine=google q=coffee --jq ".organic_results[:3]"
  serpapi search engine=google q=coffee --all-pages --max-pages 3`,
	Args: cobra.ArbitraryArgs,
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&allPagesFlag, "all-pages", false, "Fetch all pages and merge array results")
	searchCmd.Flags().IntVar(&maxPagesFlag, "max-pages", 0, "Maximum number of pages to fetch when paginating with --all-pages")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	parsed, err := params.ParseAll(args)
	if err != nil {
		return err
	}

	paramsMap := params.ParamsToMap(parsed)
	params.ApplyFields(paramsMap, fieldsFlag)

	// Search runs without a key, as in v0.2: the API answers a few demo
	// queries without one and returns 401 for the rest.
	apiKey, keyErr := resolveAPIKey()
	var cfgErr *clierrors.UsageError
	if keyErr != config.ErrNoAPIKey && errors.As(keyErr, &cfgErr) {
		warn(cmd, "%s; searching without a key", cfgErr.Message)
	}
	client := newClient(apiKey)

	hasMaxPages := cmd.Flags().Changed("max-pages")

	if hasMaxPages && !allPagesFlag {
		warn(cmd, "--max-pages has no effect without --all-pages")
	}

	if !allPagesFlag {
		raw, err := withSpinner("Searching...", func() (json.RawMessage, error) { return client.Search(cmd.Context(), paramsMap) })
		if err != nil {
			return err
		}
		return handleOutput(cmd, raw)
	}

	// Pagination mode
	maxPages := defaultMaxPages
	if hasMaxPages {
		maxPages = maxPagesFlag
	}

	paginationRestrictor := withPaginationRestrictor(paramsMap["json_restrictor"])
	if paginationRestrictor != "" {
		paramsMap["json_restrictor"] = paginationRestrictor
	}

	currentParams := paramsMap
	var accumulated map[string]any
	visitedPages := make(map[string]bool)
	pagesFetched := 0
	visitedPages[canonicalParamsKey(currentParams)] = true

	for {
		raw, err := withSpinner(fmt.Sprintf("Fetching page %d...", pagesFetched+1), func() (json.RawMessage, error) {
			return client.Search(cmd.Context(), currentParams)
		})
		if err != nil {
			return err
		}
		pagesFetched++

		var result map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&result); err != nil {
			return &clierrors.APIError{Message: "Failed to parse response: " + err.Error()}
		}

		nextURL := extractNextURL(result)

		if accumulated == nil {
			accumulated = result
		} else {
			mergeArrayFields(accumulated, result)
		}

		if nextURL == "" {
			break
		}

		if pagesFetched >= maxPages {
			break
		}

		nextParams, err := parseNextParams(nextURL, paginationRestrictor)
		if err != nil {
			return err
		}

		canonical := canonicalParamsKey(nextParams)
		if visitedPages[canonical] {
			break
		}
		visitedPages[canonical] = true
		currentParams = nextParams
	}

	if accumulated == nil {
		return &clierrors.APIError{Message: "No results returned"}
	}

	delete(accumulated, "serpapi_pagination")

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(accumulated); err != nil {
		return &clierrors.APIError{Message: "Failed to encode result: " + err.Error()}
	}
	return handleOutput(cmd, json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")))
}

// withPaginationRestrictor adds the pagination field needed internally while
// preserving the caller's server-side field projection.
func withPaginationRestrictor(restrictor string) string {
	if restrictor == "" {
		return ""
	}
	return restrictor + "," + paginationRestrictorPath
}

// extractNextURL pulls the next pagination URL from a search result.
func extractNextURL(result map[string]any) string {
	pag, ok := result["serpapi_pagination"]
	if !ok {
		return ""
	}
	pagMap, ok := pag.(map[string]any)
	if !ok {
		return ""
	}
	next, ok := pagMap["next"]
	if !ok {
		return ""
	}
	s, _ := next.(string)
	return s
}

// mergeArrayFields appends array values from src into dst; scalar fields are overwritten.
func mergeArrayFields(dst, src map[string]any) {
	for key, val := range src {
		if newItems, ok := val.([]any); ok {
			if existing, ok := dst[key]; ok {
				if existingArr, ok := existing.([]any); ok {
					dst[key] = append(existingArr, newItems...)
					continue
				}
			}
		}
		dst[key] = val
	}
}

// parseNextParams extracts query parameters from a full URL and reapplies the
// internal restrictor because pagination URLs may omit response-shaping params.
func parseNextParams(nextURL, jsonRestrictor string) (map[string]string, error) {
	parsed, err := url.Parse(nextURL)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid pagination URL: " + err.Error()}
	}
	result := make(map[string]string)
	for k, v := range parsed.Query() {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	if jsonRestrictor != "" {
		result["json_restrictor"] = jsonRestrictor
	}
	return result, nil
}

// canonicalParamsKey produces a stable key from query params regardless of order.
func canonicalParamsKey(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := url.Values{}
	for _, k := range keys {
		vals.Set(k, p[k])
	}
	return vals.Encode()
}

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/params"
)

var (
	allPagesFlag bool
	maxPagesFlag int
)

const defaultMaxPages = 100

var searchCmd = &cobra.Command{
	Use:   "search [PARAMS...]",
	Short: "Perform a search with any supported SerpApi engine",
	Args:  cobra.ArbitraryArgs,
	RunE:  runSearch,
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

	apiKey := resolveAPIKeyOptional()
	paramsMap := params.ParamsToMap(parsed)
	params.ApplyFields(paramsMap, fieldsFlag)

	hasMaxPages := cmd.Flags().Changed("max-pages")

	if hasMaxPages && !allPagesFlag {
		fmt.Fprintln(os.Stderr, "Warning: --max-pages has no effect without --all-pages")
	}

	if !allPagesFlag {
		sp := newSpinner("Searching...")
		sp.Start()
		defer sp.Stop()
		client := api.New(apiKey)
		raw, err := client.Search(cmd.Context(), paramsMap)
		if err != nil {
			return err
		}
		return handleOutput(raw)
	}

	// Pagination mode
	maxPages := defaultMaxPages
	if hasMaxPages {
		maxPages = maxPagesFlag
	}

	client := api.New(apiKey)
	currentParams := paramsMap
	var accumulated map[string]any
	visitedPages := make(map[string]bool)
	pagesFetched := 0
	visitedPages[canonicalParamsKey(currentParams)] = true

	for {
		sp := newSpinner(fmt.Sprintf("Fetching page %d...", pagesFetched+1))
		sp.Start()
		raw, err := client.Search(cmd.Context(), currentParams)
		sp.Stop()
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

		nextParams, err := parseNextParams(nextURL)
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
	return handleOutput(json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")))
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

// parseNextParams extracts query parameters from a full URL.
func parseNextParams(nextURL string) (map[string]string, error) {
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

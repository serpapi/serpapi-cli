package params

import (
	"slices"
	"strings"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// Param represents a single key=value query parameter.
type Param struct {
	Key   string
	Value string
}

// keyParams are the query parameters SerpApi reads an API key from.
var keyParams = map[string]bool{
	"api_key":      true,
	"apikey":       true,
	"key":          true,
	"serp_api_key": true,
	"serpapi_key":  true,
}

// ParseParam parses a CLI parameter string into a Param.
// A bare word (no "=") is treated as q=<word>.
func ParseParam(s string) (Param, error) {
	if s == "" {
		return Param{}, &clierrors.UsageError{Message: "Empty parameter"}
	}

	key, value, found := strings.Cut(s, "=")
	if !found {
		return Param{Key: "q", Value: s}, nil
	}
	if key == "" {
		return Param{}, &clierrors.UsageError{Message: "Empty parameter key in " + s}
	}
	if keyParams[key] {
		return Param{}, &clierrors.UsageError{
			Message: "Pass the API key with --api-key or SERPAPI_KEY, not as the " + key + " parameter",
		}
	}
	return Param{Key: key, Value: value}, nil
}

// ParseAll parses CLI arguments into Params. Bare words are joined with spaces
// into one q parameter, so `coffee shops` searches for "coffee shops".
func ParseAll(args []string) ([]Param, error) {
	parsed := make([]Param, 0, len(args))
	query, explicitQuery := -1, false
	for _, s := range args {
		p, err := ParseParam(s)
		if err != nil {
			return nil, err
		}
		if strings.Contains(s, "=") {
			explicitQuery = explicitQuery || p.Key == "q"
			parsed = append(parsed, p)
		} else if query >= 0 {
			parsed[query].Value += " " + p.Value
		} else {
			query = len(parsed)
			parsed = append(parsed, p)
		}
	}
	if query >= 0 && explicitQuery {
		return nil, &clierrors.UsageError{
			Message: "Got both q=... and bare words (" + parsed[query].Value + "); quote the query as one q=... parameter",
		}
	}
	return parsed, nil
}

// Repeated returns the last occurrence of each key given more than once with
// different values, in the order the keys first appear.
func Repeated(params []Param) []Param {
	first := make(map[string]string, len(params))
	last := make(map[string]string, len(params))
	var order []string
	for _, p := range params {
		v, seen := first[p.Key]
		if !seen {
			first[p.Key] = p.Value
		} else if v != p.Value && !slices.Contains(order, p.Key) {
			order = append(order, p.Key)
		}
		last[p.Key] = p.Value
	}
	repeated := make([]Param, 0, len(order))
	for _, k := range order {
		repeated = append(repeated, Param{Key: k, Value: last[k]})
	}
	return repeated
}

// ParamsToMap converts a slice of Params to a map. A repeated key keeps its last value.
func ParamsToMap(params []Param) map[string]string {
	m := make(map[string]string, len(params))
	for _, p := range params {
		m[p.Key] = p.Value
	}
	return m
}

// ApplyFields sets json_restrictor from fields, which overrides a
// json_restrictor parameter, and returns the restrictor in effect ("" for none).
// Empty segments are dropped because SerpApi answers them with the whole
// response, and the extra paths (such as "error") are appended so that a
// pruned response keeps them.
func ApplyFields(params map[string]string, fields string, extra ...string) string {
	if fields != "" {
		params["json_restrictor"] = fields
	}
	restrictor, ok := params["json_restrictor"]
	if !ok {
		return ""
	}

	var segments []string
	present := make(map[string]bool)
	for _, s := range strings.Split(restrictor, ",") {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			segments = append(segments, s)
			present[trimmed] = true
		}
	}
	if len(segments) == 0 {
		delete(params, "json_restrictor")
		return ""
	}
	for _, path := range extra {
		if !present[path] {
			segments = append(segments, path)
		}
	}
	restrictor = strings.TrimSpace(strings.Join(segments, ","))
	params["json_restrictor"] = restrictor
	return restrictor
}

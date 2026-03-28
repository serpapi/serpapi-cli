package params

import (
	"strings"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// Param represents a single key=value query parameter.
type Param struct {
	Key   string
	Value string
}

// ParseParam parses a CLI parameter string into a Param.
// Bare words (no "=") are treated as q=<word>.
func ParseParam(s string) (Param, error) {
	if s == "" {
		return Param{}, &clierrors.UsageError{Message: "Empty parameter"}
	}

	idx := strings.IndexByte(s, '=')
	if idx < 0 {
		// Bare word → q=<word>
		return Param{Key: "q", Value: s}, nil
	}

	key := s[:idx]
	value := s[idx+1:]

	if key == "" {
		return Param{}, &clierrors.UsageError{Message: "Empty parameter key"}
	}
	if key == "api_key" {
		return Param{}, &clierrors.UsageError{
			Message: "Use --api-key or SERPAPI_KEY instead of passing api_key as a parameter",
		}
	}

	return Param{Key: key, Value: value}, nil
}

// ParseAll parses a slice of CLI argument strings into Params.
func ParseAll(args []string) ([]Param, error) {
	parsed := make([]Param, 0, len(args))
	for _, s := range args {
		p, err := ParseParam(s)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, p)
	}
	return parsed, nil
}

// ParamsToMap converts a slice of Params to a map.
func ParamsToMap(params []Param) map[string]string {
	m := make(map[string]string, len(params))
	for _, p := range params {
		m[p.Key] = p.Value
	}
	return m
}

// ApplyFields sets the json_restrictor parameter if fields is non-empty.
func ApplyFields(params map[string]string, fields string) {
	if fields != "" {
		params["json_restrictor"] = fields
	}
}

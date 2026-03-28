package jq

import (
	"fmt"

	"github.com/itchyny/gojq"
)

// Apply runs a jq expression against the input and returns all output values.
func Apply(expression string, input any) ([]any, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("jq parse error: %w", err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("jq compile error: %w", err)
	}

	var results []any
	iter := code.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			return nil, fmt.Errorf("jq runtime error: %w", err)
		}
		results = append(results, v)
	}
	return results, nil
}

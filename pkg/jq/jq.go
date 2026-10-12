package jq

import (
	"context"
	"errors"
	"fmt"

	"github.com/itchyny/gojq"
)

// Compile parses and compiles a jq expression, so a typo can be reported
// before a billed search runs.
func Compile(expression string) (*gojq.Code, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("jq parse error: %w", err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("jq compile error: %w", err)
	}
	return code, nil
}

// Run evaluates code against input and passes each result to emit as soon as
// it is produced, so memory stays flat however many results there are. halt
// ends the output quietly; another runtime error is returned after the
// results before it were emitted. An error from emit stops the run and is
// returned as is, and so is ctx.Err() once ctx is done.
func Run(ctx context.Context, code *gojq.Code, input any, emit func(any) error) error {
	iter := code.RunWithContext(ctx, input)
	for {
		v, ok := iter.Next()
		if !ok {
			return nil
		}
		if err, isErr := v.(error); isErr {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return ctx.Err()
			}
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				return nil
			}
			return fmt.Errorf("jq runtime error: %w", err)
		}
		if err := emit(v); err != nil {
			return err
		}
	}
}

// Apply runs a jq expression against the input and returns all output values.
func Apply(expression string, input any) ([]any, error) {
	code, err := Compile(expression)
	if err != nil {
		return nil, err
	}
	var results []any
	err = Run(context.Background(), code, input, func(v any) error {
		results = append(results, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

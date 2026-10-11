package jq

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestApplyIdentity(t *testing.T) {
	input := map[string]any{"a": float64(1)}
	results, err := Apply(".", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	m, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", results[0])
	}
	if m["a"] != float64(1) {
		t.Errorf("expected a=1, got %v", m["a"])
	}
}

func TestApplyFieldAccess(t *testing.T) {
	input := map[string]any{"name": "test"}
	results, err := Apply(".name", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0] != "test" {
		t.Errorf("expected 'test', got %v", results[0])
	}
}

func TestApplyArrayIndex(t *testing.T) {
	input := []any{"a", "b", "c"}
	results, err := Apply(".[1]", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0] != "b" {
		t.Errorf("expected 'b', got %v", results[0])
	}
}

func TestApplyMultipleOutputs(t *testing.T) {
	input := map[string]any{"a": float64(1), "b": float64(2)}
	results, err := Apply(".a, .b", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestApplyInvalidExpression(t *testing.T) {
	input := map[string]any{}
	_, err := Apply("invalid[[[", input)
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestApplyHaltStopsWithoutError(t *testing.T) {
	results, err := Apply("1, halt, 2", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != 1 {
		t.Errorf("results = %v, want [1]", results)
	}
}

func TestApplyHaltErrorFails(t *testing.T) {
	if _, err := Apply(`"boom" | halt_error`, nil); err == nil {
		t.Error("halt_error should be an error")
	}
}

func TestCompileReportsParseAndCompileErrors(t *testing.T) {
	if _, err := Compile(".organic_results[].title"); err != nil {
		t.Fatalf("valid expression: %v", err)
	}
	if _, err := Compile(".["); err == nil || !strings.Contains(err.Error(), "jq parse error") {
		t.Errorf("want parse error, got %v", err)
	}
	if _, err := Compile("nosuchfn(1)"); err == nil || !strings.Contains(err.Error(), "jq compile error") {
		t.Errorf("want compile error, got %v", err)
	}
}

func TestRunEmitsEachResultBeforeAnError(t *testing.T) {
	code, err := Compile(`.[] | . + 1`)
	if err != nil {
		t.Fatal(err)
	}
	var got []any
	err = Run(context.Background(), code, []any{1, 2, "x"}, func(v any) error {
		got = append(got, v)
		return nil
	})
	if len(got) != 2 || err == nil || !strings.Contains(err.Error(), "cannot add") {
		t.Errorf("got %v, %v; want [2 3] then a runtime error", got, err)
	}
}

func TestRunStopsEndlessOutputWhenEmitFails(t *testing.T) {
	code, err := Compile(`repeat(1)`)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	n := 0
	err = Run(context.Background(), code, nil, func(any) error {
		if n++; n == 1000 {
			return stop
		}
		return nil
	})
	if err != stop || n != 1000 {
		t.Errorf("Run returned %v after %d results; want the emit error after 1000", err, n)
	}
}

// A filter that computes without printing, such as a large reduce, must
// still stop on Ctrl-C.
func TestRunStopsWhenTheContextIsCanceled(t *testing.T) {
	code, err := Compile(`reduce range(1e12) as $i (0; . + 1)`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = Run(ctx, code, nil, func(any) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run returned %v, want the context's error", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Run took %s to notice the canceled context", took)
	}
}

package jq

import (
	"testing"
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

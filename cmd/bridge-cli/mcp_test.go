package main

import (
	"testing"
)

func TestMCPJSON(t *testing.T) {
	// Test successful marshaling
	result := mcpJSON(map[string]string{"key": "value"})
	if result != "{\n  \"key\": \"value\"\n}" {
		t.Errorf("unexpected JSON output: %s", result)
	}

	// Test with simple types
	result = mcpJSON(42)
	if result != "42" {
		t.Errorf("unexpected int output: %s", result)
	}

	result = mcpJSON("hello")
	if result != `"hello"` {
		t.Errorf("unexpected string output: %s", result)
	}
}

func TestMCPToolResult(t *testing.T) {
	result, err := mcpToolResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func TestMCPError(t *testing.T) {
	result, err := mcpError("something went wrong")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func TestMCPErrorf(t *testing.T) {
	result, err := mcpErrorf("error: %s, code: %d", "not found", 404)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

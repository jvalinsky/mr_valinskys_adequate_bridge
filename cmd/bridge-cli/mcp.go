// Package main provides MCP (Model Context Protocol) server infrastructure
// for the bridge CLI, enabling structured tool access from AI assistants.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// mcpJSON marshals v to indented JSON for MCP tool result text content.
func mcpJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// mcpToolResult creates a text tool result from a JSON-serializable value.
func mcpToolResult(v any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(mcpJSON(v)), nil
}

// mcpError creates an error tool result with the given message.
func mcpError(msg string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(msg), nil
}

// mcpErrorf creates a formatted error tool result.
func mcpErrorf(format string, args ...any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

// plcURLFlag is the common flag name for the PLC directory URL.
const plcURLFlag = "plc-url"

// appviewURLFlag is the common flag name for the ATProto AppView URL.
const appviewURLFlag = "appview-url"

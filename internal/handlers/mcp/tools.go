package mcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTool[In, Out any](server *mcpsdk.Server, tool *mcpsdk.Tool, handler mcpsdk.ToolHandlerFor[In, Out]) {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic("mcp: infer tool input schema: " + err.Error())
	}
	// Hermes' identity plugin adds a trusted, non-model argument to every Family Assistant call.
	// Keep the public properties unchanged while allowing that private envelope through validation.
	schema.AdditionalProperties = nil
	tool.InputSchema = schema

	mcpsdk.AddTool(server, tool, func(ctx context.Context, request *mcpsdk.CallToolRequest, input In) (*mcpsdk.CallToolResult, Out, error) {
		trustedContext, err := toolIdentityContext(ctx, request)
		if err != nil {
			var zero Out
			return nil, zero, MapToolError(err)
		}
		return handler(trustedContext, request, input)
	})
}

package mcp

import (
	"context"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTool[In, Out any](server *mcpsdk.Server, tool *mcpsdk.Tool, handler func(context.Context, In) (Out, error)) {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic("mcp: infer tool input schema: " + err.Error())
	}
	if schema == nil {
		panic("mcp: inferred tool input schema is nil")
	}
	// Hermes' identity plugin adds a trusted, non-model argument to every Family Assistant call.
	// Keep the public properties unchanged while allowing that private envelope through validation.
	schema.AdditionalProperties = nil
	tool.InputSchema = schema

	mcpsdk.AddTool(server, tool, func(ctx context.Context, request *mcpsdk.CallToolRequest, input In) (*mcpsdk.CallToolResult, Out, error) {
		startedAt := time.Now()
		trustedContext, err := toolIdentityContext(ctx, request)
		if err != nil {
			var zero Out
			logMCPToolCall(ctx, tool.Name, input, zero, time.Since(startedAt), err)
			return nil, zero, MapToolError(err)
		}
		output, err := handler(trustedContext, input)
		logMCPToolCall(trustedContext, tool.Name, input, output, time.Since(startedAt), err)
		return nil, output, err
	})
}

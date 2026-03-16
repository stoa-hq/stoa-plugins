package stripe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// toolAdder is the interface satisfied by both *server.MCPServer and
// *mcp.ScopedMCPServer (which enforces tool name prefixes).
type toolAdder interface {
	AddTool(mcp.Tool, server.ToolHandlerFunc)
}

// RegisterStoreMCPTools implements sdk.MCPStorePlugin.
// It adds Stripe tools to the Store MCP server.
func (p *Plugin) RegisterStoreMCPTools(srv any, client sdk.StoreAPIClient) {
	s := srv.(toolAdder)
	t, h := stripeCreatePaymentIntentTool(client)
	s.AddTool(t, h)
	t2, h2 := stripeCreatePreOrderPaymentIntentTool(client)
	s.AddTool(t2, h2)
}

func stripeCreatePaymentIntentTool(client sdk.StoreAPIClient) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("store_stripe_create_payment_intent",
		mcp.WithDescription(
			"Create a Stripe PaymentIntent for a pending order. "+
				"Returns a client_secret (for Stripe.js / mobile SDKs) and the publishable_key. "+
				"Call store_checkout first to create the order, then call this tool to initiate payment.",
		),
		mcp.WithString("order_id",
			mcp.Description("UUID of the pending order to pay for"),
			mcp.Required(),
		),
		mcp.WithString("payment_method_id",
			mcp.Description("UUID of the Stoa PaymentMethod configured for Stripe (provider = stripe)"),
			mcp.Required(),
		),
	)
	handler := func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]interface{}{
			"order_id":          req.GetString("order_id", ""),
			"payment_method_id": req.GetString("payment_method_id", ""),
		}
		data, err := client.Post("/api/v1/store/stripe/payment-intent", body)
		if err != nil {
			// Return a sanitized error to MCP consumers — do not leak
			// internal URLs, connection details, or Stripe error specifics.
			return mcp.NewToolResultError("failed to create payment intent"), nil
		}
		return formatMCPResult(data), nil
	}
	return tool, handler
}

func stripeCreatePreOrderPaymentIntentTool(client sdk.StoreAPIClient) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("store_stripe_create_preorder_payment_intent",
		mcp.WithDescription(
			"Create a Stripe PaymentIntent before placing an order (pay-first flow). "+
				"Returns a payment_intent_id to pass as payment_reference to store_checkout.",
		),
		mcp.WithNumber("amount",
			mcp.Description("Total amount in cents (e.g. 4999 for €49.99)"),
			mcp.Required(),
		),
		mcp.WithString("currency",
			mcp.Description("Three-letter currency code (e.g. EUR)"),
			mcp.Required(),
		),
		mcp.WithString("payment_method_id",
			mcp.Description("UUID of the Stoa PaymentMethod configured for Stripe (provider = stripe)"),
			mcp.Required(),
		),
	)
	handler := func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		amountRaw, _ := args["amount"].(float64)
		if amountRaw <= 0 {
			return mcp.NewToolResultError("amount must be positive"), nil
		}
		body := map[string]interface{}{
			"amount":            int64(amountRaw),
			"currency":          req.GetString("currency", ""),
			"payment_method_id": req.GetString("payment_method_id", ""),
		}
		data, err := client.Post("/api/v1/store/stripe/payment-intent", body)
		if err != nil {
			return mcp.NewToolResultError("failed to create pre-order payment intent"), nil
		}
		return formatMCPResult(data), nil
	}
	return tool, handler
}

// formatMCPResult pretty-prints a Stoa API response for MCP consumers.
func formatMCPResult(data []byte) *mcp.CallToolResult {
	var pretty json.RawMessage
	if err := json.Unmarshal(data, &pretty); err != nil {
		return mcp.NewToolResultText(string(data))
	}
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(data))
	}
	return mcp.NewToolResultText(fmt.Sprintf("%s", out))
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// jsonRPCVersion is the only protocol version MCP speaks over the wire.
const jsonRPCVersion = "2.0"

// rpcRequest is a JSON-RPC 2.0 request or notification. A request carries an ID
// the server echoes on its response; a notification omits ID (it is zero, elided
// by omitempty) and expects no reply. IDs start at 1 so the zero value never
// names a real request — a decoded response with ID 0 is therefore a
// server-initiated notification, which the client ignores.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response: exactly one of Result or Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object. It is a protocol-level failure (unknown
// method, invalid params) the server reports for a specific call — distinct from
// a transport failure (a dead connection) and from a tool's own domain error
// (carried in a successful tools/call result with isError set). The client
// surfaces an rpcError to the model but keeps the connection healthy.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message)
}

// transport carries JSON-RPC traffic to one MCP server. call sends a request and
// waits for its matching response; notify fires a one-way notification; close
// tears the connection down. Implementations own request-ID assignment and
// response correlation so the Client need not. A transport must be safe for
// concurrent calls — parallel tool execution (AS-019) may invoke several at once.
type transport interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close() error
}

// marshalParams encodes a params value to raw JSON, treating a nil params as an
// absent field (no params object on the wire).
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode params: %w", err)
	}
	return raw, nil
}

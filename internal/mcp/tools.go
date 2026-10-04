package mcp

import (
	"encoding/json"
	"fmt"
)

// defaultProtocolVersion is used when the client omits protocolVersion.
const defaultProtocolVersion = "2025-06-18"

// tool describes one MCP tool with its JSON Schema inputSchema.
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Tool input schemas (compact, valid JSON Schema).
const (
	reportActivitySchema = `{"type":"object","properties":{"kind":{"type":"string","enum":["READ","EDIT","COMMAND","TEST","COMMIT"]},"command":{"type":"string"},"exit_code":{"type":"integer"},"stderr":{"type":"string"},"files":{"type":"array","items":{"type":"string"}}},"required":["kind"],"additionalProperties":false}`
	checkHealthSchema    = `{"type":"object","properties":{},"additionalProperties":false}`
	createHandoffSchema  = `{"type":"object","properties":{"goal":{"type":"string"},"decisions":{"type":"string"},"completed_work":{"type":"string"},"blocker":{"type":"string"},"failed_attempts":{"type":"array","items":{"type":"string"}},"next_action":{"type":"string"}},"required":["goal","next_action"],"additionalProperties":false}`
)

// tools is the fixed tool list exposed by tools/list.
var tools = []tool{
	{
		Name:        "report_activity",
		Description: "Append one activity record (kind, command, exit code, stderr, touched files) to Vault's activity log.",
		InputSchema: json.RawMessage(reportActivitySchema),
	},
	{
		Name:        "check_context_health",
		Description: "Check AI-agent context health. STUB in phase 1: always score 100.",
		InputSchema: json.RawMessage(checkHealthSchema),
	},
	{
		Name:        "create_handoff",
		Description: "Write a handoff state file from the session summary and activity log.",
		InputSchema: json.RawMessage(createHandoffSchema),
	},
}

// handleInitialize responds to the MCP initialize request, echoing the
// client's protocolVersion (default "2025-06-18").
func (s *Server) handleInitialize(id json.RawMessage, params json.RawMessage) response {
	pv := defaultProtocolVersion
	if len(params) > 0 && string(params) != "null" {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			pv = p.ProtocolVersion
		}
	}
	result, err := json.Marshal(map[string]any{
		"protocolVersion": pv,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "vault", "version": "0.1.0"},
	})
	if err != nil {
		s.log.Printf("marshal initialize: %v", err)
	}
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

// handleToolsList responds with the full tool list.
func (s *Server) handleToolsList(id json.RawMessage) response {
	result, err := json.Marshal(map[string]any{"tools": tools})
	if err != nil {
		s.log.Printf("marshal tools/list: %v", err)
	}
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

// handleToolsCall dispatches a tools/call to state and wraps the text output.
// Tool-level failures are reported with isError:true, never a JSON-RPC error.
func (s *Server) handleToolsCall(id json.RawMessage, params json.RawMessage) response {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return s.toolResult(id, fmt.Sprintf("invalid tools/call params: %v", err), true)
		}
	}
	text, isErr := s.st.DispatchTool(p.Name, p.Arguments)
	return s.toolResult(id, text, isErr)
}

// toolResult builds the tools/call result shape.
func (s *Server) toolResult(id json.RawMessage, text string, isErr bool) response {
	result, err := json.Marshal(map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	})
	if err != nil {
		s.log.Printf("marshal tools/call result: %v", err)
	}
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

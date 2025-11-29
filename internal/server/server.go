package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jc/gdpr-mcp/internal/db"
	"github.com/jc/gdpr-mcp/internal/ingest"
)

// JSON-RPC 2.0 structures with proper serialization

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse handles proper serialization of result OR error
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP Protocol structures

type MCPInitializeResult struct {
	ProtocolVersion string                `json:"protocolVersion"`
	Capabilities    MCPServerCapabilities `json:"capabilities"`
	ServerInfo      MCPImplementation     `json:"serverInfo"`
}

type MCPServerCapabilities struct {
	Tools *MCPToolsCapability `json:"tools,omitempty"`
}

type MCPToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type MCPImplementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"inputSchema"`
}

type MCPToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

type MCPToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type MCPCallToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// JSON Schema for tool input
type JSONSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// Server config

type Config struct {
	DBPath      string
	UseOpenAI   bool
	OpenAIKey   string
	OpenAIModel string
}

// Server handles MCP requests
type Server struct {
	db     *db.DB
	config Config
}

// New creates a new MCP server
func New(database *db.DB, config Config) *Server {
	return &Server{
		db:     database,
		config: config,
	}
}

// Run starts the JSON-RPC server on stdin/stdout
func (s *Server) Run() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read input: %w", err)
		}

		// Skip empty lines
		if len(line) == 0 || (len(line) == 1 && line[0] == '\n') {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "Parse error", err.Error())
			continue
		}

		// Parse the ID - keep it as raw JSON to preserve type
		var reqID interface{}
		if len(req.ID) > 0 {
			if err := json.Unmarshal(req.ID, &reqID); err != nil {
				reqID = nil
			}
		}

		// Handle the request
		s.handleRequest(req.Method, reqID, req.Params)
	}
}

func (s *Server) handleRequest(method string, id interface{}, params json.RawMessage) {
	switch method {
	case "initialize":
		s.handleInitialize(id, params)
	case "initialized":
		// Notification - no response needed
		return
	case "notifications/initialized":
		// Alternative notification format - no response needed
		return
	case "tools/list":
		s.handleToolsList(id)
	case "tools/call":
		s.handleToolsCall(id, params)
	case "ping":
		s.handlePing(id)
	default:
		s.writeError(id, -32601, "Method not found", method)
	}
}

func (s *Server) handleInitialize(id interface{}, params json.RawMessage) {
	result := MCPInitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: MCPServerCapabilities{
			Tools: &MCPToolsCapability{
				ListChanged: false,
			},
		},
		ServerInfo: MCPImplementation{
			Name:    "gdpr-mcp",
			Version: "1.0.0",
		},
	}

	s.writeResult(id, result)
}

func (s *Server) handleToolsList(id interface{}) {
	tools := []MCPTool{
		{
			Name:        "gdpr_search",
			Description: "Search GDPR documents using hybrid trigram and vector search",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query string",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default: 10)",
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "gdpr_get",
			Description: "Get a specific GDPR document chunk by ID",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "integer",
						"description": "Document chunk ID",
					},
				},
				Required: []string{"id"},
			},
		},
	}

	s.writeResult(id, MCPToolsListResult{Tools: tools})
}

func (s *Server) handleToolsCall(id interface{}, params json.RawMessage) {
	var toolParams MCPToolCallParams
	if err := json.Unmarshal(params, &toolParams); err != nil {
		s.writeError(id, -32602, "Invalid params", err.Error())
		return
	}

	switch toolParams.Name {
	case "gdpr_search":
		s.handleSearchTool(id, toolParams.Arguments)
	case "gdpr_get":
		s.handleGetTool(id, toolParams.Arguments)
	default:
		s.writeError(id, -32602, "Unknown tool", toolParams.Name)
	}
}

func (s *Server) handleSearchTool(id interface{}, args json.RawMessage) {
	var searchArgs struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	if err := json.Unmarshal(args, &searchArgs); err != nil {
		s.writeToolError(id, "Invalid arguments: "+err.Error())
		return
	}

	if searchArgs.Query == "" {
		s.writeToolError(id, "Query is required")
		return
	}

	if searchArgs.Limit <= 0 {
		searchArgs.Limit = 10
	}

	// Generate query embedding for hybrid search
	var queryEmbedding []float32
	if s.config.UseOpenAI && s.config.OpenAIKey != "" {
		var err error
		queryEmbedding, err = ingest.EmbedQuery(
			searchArgs.Query,
			true,
			s.config.OpenAIKey,
			s.config.OpenAIModel,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate query embedding: %v\n", err)
		}
	} else {
		queryEmbedding, _ = ingest.EmbedQuery(searchArgs.Query, false, "", "")
	}

	results, err := s.db.HybridSearch(searchArgs.Query, queryEmbedding, searchArgs.Limit)
	if err != nil {
		s.writeToolError(id, "Search failed: "+err.Error())
		return
	}

	resultJSON, err := json.Marshal(results)
	if err != nil {
		s.writeToolError(id, "Failed to marshal results: "+err.Error())
		return
	}

	s.writeToolResult(id, string(resultJSON))
}

func (s *Server) handleGetTool(id interface{}, args json.RawMessage) {
	var getArgs struct {
		ID int64 `json:"id"`
	}

	if err := json.Unmarshal(args, &getArgs); err != nil {
		s.writeToolError(id, "Invalid arguments: "+err.Error())
		return
	}

	if getArgs.ID <= 0 {
		s.writeToolError(id, "Valid document ID is required")
		return
	}

	doc, err := s.db.GetDocument(getArgs.ID)
	if err != nil {
		s.writeToolError(id, "Failed to get document: "+err.Error())
		return
	}

	if doc == nil {
		s.writeToolError(id, "Document not found")
		return
	}

	result := map[string]interface{}{
		"id":          doc.ID,
		"chunk":       doc.Chunk,
		"chunk_index": doc.ChunkIndex,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.writeToolError(id, "Failed to marshal result: "+err.Error())
		return
	}

	s.writeToolResult(id, string(resultJSON))
}

func (s *Server) handlePing(id interface{}) {
	s.writeResult(id, map[string]interface{}{})
}

// Response writers

func (s *Server) writeResult(id interface{}, result interface{}) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	s.writeJSON(resp)
}

func (s *Server) writeError(id interface{}, code int, message string, data interface{}) {
	errorObj := map[string]interface{}{
		"code":    code,
		"message": message,
	}
	if data != nil {
		errorObj["data"] = data
	}

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   errorObj,
	}
	s.writeJSON(resp)
}

func (s *Server) writeToolResult(id interface{}, text string) {
	result := MCPCallToolResult{
		Content: []MCPContent{
			{Type: "text", Text: text},
		},
	}
	s.writeResult(id, result)
}

func (s *Server) writeToolError(id interface{}, message string) {
	result := MCPCallToolResult{
		Content: []MCPContent{
			{Type: "text", Text: message},
		},
		IsError: true,
	}
	s.writeResult(id, result)
}

func (s *Server) writeJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal response: %v\n", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

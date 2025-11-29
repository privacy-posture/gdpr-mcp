package server

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jc/gdpr-mcp/internal/db"
)

func setupTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gdpr-mcp-server-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	if err := database.Migrate(); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Insert test data
	testDocs := []struct {
		chunk     string
		embedding []float32
	}{
		{
			chunk:     "Article 15 - Right of access by the data subject. The data subject shall have the right to obtain from the controller confirmation.",
			embedding: []float32{1.0, 0.5, 0.0},
		},
		{
			chunk:     "Article 17 - Right to erasure ('right to be forgotten'). The data subject shall have the right to obtain erasure.",
			embedding: []float32{0.8, 0.6, 0.0},
		},
		{
			chunk:     "Article 20 - Right to data portability. The data subject shall have the right to receive personal data.",
			embedding: []float32{0.7, 0.7, 0.1},
		},
	}

	for i, d := range testDocs {
		docID, err := database.InsertChunk(d.chunk, i)
		if err != nil {
			database.Close()
			os.RemoveAll(tmpDir)
			t.Fatalf("Failed to insert chunk: %v", err)
		}

		trigrams := db.GenerateTrigrams(d.chunk)
		if err := database.InsertTrigrams(docID, trigrams); err != nil {
			database.Close()
			os.RemoveAll(tmpDir)
			t.Fatalf("Failed to insert trigrams: %v", err)
		}

		if err := database.InsertEmbedding(docID, d.embedding); err != nil {
			database.Close()
			os.RemoveAll(tmpDir)
			t.Fatalf("Failed to insert embedding: %v", err)
		}
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, cleanup
}

// captureServerOutput runs a server request and captures the JSON output
func captureServerOutput(t *testing.T, srv *Server, request string) map[string]interface{} {
	t.Helper()

	// Save original stdout
	oldStdout := os.Stdout

	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stdout = w

	// Parse request
	var req JSONRPCRequest
	if err := json.Unmarshal([]byte(request), &req); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("Failed to parse request: %v", err)
	}

	// Parse ID
	var reqID interface{}
	if len(req.ID) > 0 {
		json.Unmarshal(req.ID, &reqID)
	}

	// Handle request
	srv.handleRequest(req.Method, reqID, req.Params)

	// Close writer and restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	output := strings.TrimSpace(buf.String())
	if output == "" {
		return nil // No response (e.g., for notifications)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v\nOutput: %s", err, output)
	}

	return resp
}

func TestServerInitialize(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] != nil {
		t.Fatalf("Unexpected error: %+v", resp["error"])
	}

	if resp["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", resp["jsonrpc"])
	}

	if resp["id"] != float64(1) {
		t.Errorf("Expected id 1, got %v", resp["id"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got %T", resp["result"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected serverInfo object, got %T", result["serverInfo"])
	}

	if serverInfo["name"] != "gdpr-mcp" {
		t.Errorf("Expected server name 'gdpr-mcp', got %s", serverInfo["name"])
	}
}

func TestServerInitializedNotification(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","method":"initialized"}`
	resp := captureServerOutput(t, srv, request)

	// initialized is a notification, should return nil
	if resp != nil {
		t.Error("Expected nil response for notification")
	}
}

func TestServerToolsList(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] != nil {
		t.Fatalf("Unexpected error: %+v", resp["error"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got %T", resp["result"])
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools array, got %T", result["tools"])
	}

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolMap := tool.(map[string]interface{})
		toolNames[toolMap["name"].(string)] = true
	}

	if !toolNames["gdpr_search"] {
		t.Error("Expected 'gdpr_search' tool")
	}

	if !toolNames["gdpr_get"] {
		t.Error("Expected 'gdpr_get' tool")
	}
}

func TestServerSearchTool(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"gdpr_search","arguments":{"query":"right of access","limit":10}}}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] != nil {
		t.Fatalf("Unexpected error: %+v", resp["error"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got %T", resp["result"])
	}

	content, ok := result["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content array, got %T", result["content"])
	}

	if len(content) == 0 {
		t.Fatal("Expected content in result")
	}

	// Check isError is not set or false
	if isError, ok := result["isError"].(bool); ok && isError {
		t.Errorf("Tool returned error: %v", content)
	}
}

func TestServerGetTool(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"gdpr_get","arguments":{"id":1}}}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] != nil {
		t.Fatalf("Unexpected error: %+v", resp["error"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got %T", resp["result"])
	}

	content, ok := result["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content array, got %T", result["content"])
	}

	if len(content) == 0 {
		t.Fatal("Expected content in result")
	}
}

func TestServerGetToolNotFound(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"gdpr_get","arguments":{"id":99999}}}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got %T", resp["result"])
	}

	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Error("Expected isError to be true for non-existent document")
	}
}

func TestServerUnknownMethod(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] == nil {
		t.Fatal("Expected error for unknown method")
	}

	errorObj := resp["error"].(map[string]interface{})
	if errorObj["code"] != float64(-32601) {
		t.Errorf("Expected error code -32601, got %v", errorObj["code"])
	}
}

func TestServerUnknownTool(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] == nil {
		t.Fatal("Expected error for unknown tool")
	}
}

func TestServerPing(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":8,"method":"ping"}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp["error"] != nil {
		t.Fatalf("Unexpected error: %+v", resp["error"])
	}

	if resp["result"] == nil {
		t.Error("Expected result in ping response")
	}
}

func TestServerSearchToolMissingQuery(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"gdpr_search","arguments":{"limit":10}}}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result object, got %T", resp["result"])
	}

	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Error("Expected isError to be true for missing query")
	}
}

func TestJSONRPCResponseFormat(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	// Test that responses have correct JSON-RPC 2.0 format
	request := `{"jsonrpc":"2.0","id":"string-id","method":"ping"}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	// Check jsonrpc field
	if resp["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc '2.0', got %v", resp["jsonrpc"])
	}

	// Check id is preserved (including type)
	if resp["id"] != "string-id" {
		t.Errorf("Expected id 'string-id', got %v", resp["id"])
	}

	// Check result is present
	if resp["result"] == nil {
		t.Error("Expected result field")
	}

	// Check error is NOT present when result is present
	if resp["error"] != nil {
		t.Error("Error should not be present when result is present")
	}
}

func TestToolInputSchemaFormat(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	srv := New(database, Config{})

	request := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	resp := captureServerOutput(t, srv, request)

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	result := resp["result"].(map[string]interface{})
	tools := result["tools"].([]interface{})

	for _, tool := range tools {
		toolMap := tool.(map[string]interface{})
		schema, ok := toolMap["inputSchema"].(map[string]interface{})
		if !ok {
			t.Errorf("Tool %s missing inputSchema", toolMap["name"])
			continue
		}

		// Check required schema fields
		if schema["type"] != "object" {
			t.Errorf("Tool %s schema type should be 'object'", toolMap["name"])
		}

		if schema["properties"] == nil {
			t.Errorf("Tool %s schema missing properties", toolMap["name"])
		}
	}
}

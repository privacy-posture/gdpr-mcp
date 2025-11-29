package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jc/gdpr-mcp/internal/db"
)

func setupTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gdpr-mcp-ingest-test-*")
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

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, cleanup
}

func TestChunking(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	config := Config{
		ChunkSize:    100,
		ChunkOverlap: 20,
		UseOpenAI:    false,
	}

	ingester := New(database, config)

	// Create test text
	text := "This is sentence one. This is sentence two. This is sentence three. " +
		"This is sentence four. This is sentence five. This is sentence six. " +
		"This is sentence seven. This is sentence eight. This is sentence nine. " +
		"This is sentence ten. This is the final sentence."

	chunks := ingester.chunkText(text)

	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}

	// Verify chunks have content
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Errorf("Chunk %d is empty", i)
		}
	}

	// Verify chunks cover the text (no content lost)
	totalLen := 0
	for _, chunk := range chunks {
		totalLen += len(chunk)
	}

	// Total length should be at least the original text length
	// (will be more due to overlap)
	if totalLen < len(text) {
		t.Errorf("Total chunk length %d is less than original text length %d", totalLen, len(text))
	}
}

func TestChunkingEmptyText(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	config := DefaultConfig()
	ingester := New(database, config)

	chunks := ingester.chunkText("")
	if len(chunks) != 0 {
		t.Errorf("Expected no chunks for empty text, got %d", len(chunks))
	}
}

func TestChunkingShortText(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	config := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		UseOpenAI:    false,
	}

	ingester := New(database, config)

	text := "Short text."
	chunks := ingester.chunkText(text)

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk for short text, got %d", len(chunks))
	}

	if chunks[0] != text {
		t.Errorf("Chunk content mismatch: got %q, want %q", chunks[0], text)
	}
}

func TestIngestText(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	config := Config{
		ChunkSize:    200,
		ChunkOverlap: 50,
		UseOpenAI:    false,
	}

	ingester := New(database, config)

	text := `Article 15 - Right of access by the data subject.

The data subject shall have the right to obtain from the controller confirmation
as to whether or not personal data concerning him or her are being processed,
and, where that is the case, access to the personal data and the following information:

(a) the purposes of the processing;
(b) the categories of personal data concerned;
(c) the recipients or categories of recipient to whom the personal data have been
    or will be disclosed.`

	err := ingester.IngestText(text)
	if err != nil {
		t.Fatalf("IngestText failed: %v", err)
	}

	// Verify metadata was set
	count, err := database.GetMetadata("chunk_count")
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if count == "" || count == "0" {
		t.Error("Expected positive chunk count in metadata")
	}

	// Verify we can search the content
	results, err := database.SearchTrigrams("data subject", 10)
	if err != nil {
		t.Fatalf("SearchTrigrams failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected search results after ingestion")
	}
}

func TestIngestFile(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a temp file with test content
	tmpFile, err := os.CreateTemp("", "gdpr-test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testContent := `GDPR Article 17 - Right to erasure ('right to be forgotten')

1. The data subject shall have the right to obtain from the controller the erasure
of personal data concerning him or her without undue delay and the controller shall
have the obligation to erase personal data without undue delay.`

	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	config := DefaultConfig()
	config.UseOpenAI = false
	config.ChunkSize = 200

	ingester := New(database, config)

	err = ingester.IngestFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("IngestFile failed: %v", err)
	}

	// Verify we can find the content
	results, err := database.SearchTrigrams("erasure", 10)
	if err != nil {
		t.Fatalf("SearchTrigrams failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected search results for 'erasure'")
	}
}

func TestStubEmbedding(t *testing.T) {
	text := "Test embedding generation"
	embedding := stubEmbedding(text)

	// Check embedding dimension
	if len(embedding) != 384 {
		t.Errorf("Expected embedding dimension 384, got %d", len(embedding))
	}

	// Check that it's not all zeros
	hasNonZero := false
	for _, v := range embedding {
		if v != 0 {
			hasNonZero = true
			break
		}
	}

	if !hasNonZero {
		t.Error("Expected non-zero embedding values")
	}

	// Check consistency - same text should give same embedding
	embedding2 := stubEmbedding(text)
	for i := range embedding {
		if embedding[i] != embedding2[i] {
			t.Error("Stub embedding should be deterministic")
			break
		}
	}

	// Different text should give different embedding
	embedding3 := stubEmbedding("Different text")
	same := true
	for i := range embedding {
		if embedding[i] != embedding3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("Different text should produce different embeddings")
	}
}

func TestEmbedQuery(t *testing.T) {
	query := "right of access"

	// Test stub embedding
	embedding, err := EmbedQuery(query, false, "", "")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	if len(embedding) != 384 {
		t.Errorf("Expected embedding dimension 384, got %d", len(embedding))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.ChunkSize != 1000 {
		t.Errorf("Expected ChunkSize 1000, got %d", config.ChunkSize)
	}

	if config.ChunkOverlap != 100 {
		t.Errorf("Expected ChunkOverlap 100, got %d", config.ChunkOverlap)
	}

	if config.UseOpenAI != false {
		t.Error("Expected UseOpenAI to be false by default")
	}

	if config.OpenAIModel != "text-embedding-3-small" {
		t.Errorf("Expected OpenAIModel 'text-embedding-3-small', got %s", config.OpenAIModel)
	}
}

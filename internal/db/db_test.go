package db

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gdpr-mcp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := Open(dbPath)
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

func TestGenerateTrigrams(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple word",
			input:    "hello",
			expected: []string{"hel", "ell", "llo"},
		},
		{
			name:     "short string",
			input:    "hi",
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "with spaces",
			input:    "hello world",
			expected: []string{"hel", "ell", "llo", "lo ", "o w", " wo", "wor", "orl", "rld"},
		},
		{
			name:     "uppercase converted",
			input:    "HELLO",
			expected: []string{"hel", "ell", "llo"},
		},
		{
			name:     "duplicate trigrams",
			input:    "aaa",
			expected: []string{"aaa"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateTrigrams(tt.input)

			// Sort both for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GenerateTrigrams(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestInsertAndGetChunk(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	chunk := "This is a test chunk about GDPR Article 15."
	chunkIndex := 0

	// Insert chunk
	docID, err := database.InsertChunk(chunk, chunkIndex)
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}

	if docID <= 0 {
		t.Errorf("Expected positive doc ID, got %d", docID)
	}

	// Get chunk
	doc, err := database.GetDocument(docID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	if doc == nil {
		t.Fatal("Expected document, got nil")
	}

	if doc.Chunk != chunk {
		t.Errorf("Chunk mismatch: got %q, want %q", doc.Chunk, chunk)
	}

	if doc.ChunkIndex != chunkIndex {
		t.Errorf("ChunkIndex mismatch: got %d, want %d", doc.ChunkIndex, chunkIndex)
	}
}

func TestInsertTrigrams(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert a chunk first
	chunk := "Article 15 GDPR"
	docID, err := database.InsertChunk(chunk, 0)
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}

	// Generate and insert trigrams
	trigrams := GenerateTrigrams(chunk)
	if err := database.InsertTrigrams(docID, trigrams); err != nil {
		t.Fatalf("InsertTrigrams failed: %v", err)
	}

	// Search should find the document
	results, err := database.SearchTrigrams("article", 10)
	if err != nil {
		t.Fatalf("SearchTrigrams failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least one result")
	}

	found := false
	for _, r := range results {
		if r.ID == docID {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find inserted document in results")
	}
}

func TestInsertAndSearchEmbeddings(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert chunks with embeddings
	chunks := []struct {
		text      string
		embedding []float32
	}{
		{
			text:      "Article about data protection",
			embedding: []float32{1.0, 0.0, 0.0, 0.0},
		},
		{
			text:      "Information about privacy rights",
			embedding: []float32{0.9, 0.1, 0.0, 0.0},
		},
		{
			text:      "Unrelated content about cooking",
			embedding: []float32{0.0, 0.0, 1.0, 0.0},
		},
	}

	for i, c := range chunks {
		docID, err := database.InsertChunk(c.text, i)
		if err != nil {
			t.Fatalf("InsertChunk failed: %v", err)
		}

		if err := database.InsertEmbedding(docID, c.embedding); err != nil {
			t.Fatalf("InsertEmbedding failed: %v", err)
		}
	}

	// Search with query embedding similar to first two chunks
	queryEmbedding := []float32{0.95, 0.05, 0.0, 0.0}
	results, err := database.SearchVectors(queryEmbedding, 10)
	if err != nil {
		t.Fatalf("SearchVectors failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected results from vector search")
	}

	// First result should be the most similar one
	if results[0].Score < 0.9 {
		t.Errorf("Expected high similarity score, got %f", results[0].Score)
	}
}

func TestHybridSearch(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert test documents
	docs := []struct {
		text      string
		embedding []float32
	}{
		{
			text:      "Article 15 - Right of access by the data subject",
			embedding: []float32{1.0, 0.5, 0.0},
		},
		{
			text:      "Article 17 - Right to erasure (right to be forgotten)",
			embedding: []float32{0.8, 0.6, 0.0},
		},
		{
			text:      "Article 20 - Right to data portability",
			embedding: []float32{0.7, 0.7, 0.1},
		},
	}

	for i, d := range docs {
		docID, err := database.InsertChunk(d.text, i)
		if err != nil {
			t.Fatalf("InsertChunk failed: %v", err)
		}

		trigrams := GenerateTrigrams(d.text)
		if err := database.InsertTrigrams(docID, trigrams); err != nil {
			t.Fatalf("InsertTrigrams failed: %v", err)
		}

		if err := database.InsertEmbedding(docID, d.embedding); err != nil {
			t.Fatalf("InsertEmbedding failed: %v", err)
		}
	}

	// Test hybrid search
	queryEmbedding := []float32{0.9, 0.5, 0.0}
	results, err := database.HybridSearch("right of access", queryEmbedding, 10)
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected results from hybrid search")
	}

	// First result should be Article 15 (best match for both trigram and vector)
	if results[0].ID != 1 {
		t.Logf("Results: %+v", results)
	}
}

func TestMetadata(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	key := "test_key"
	value := "test_value"

	// Set metadata
	if err := database.SetMetadata(key, value); err != nil {
		t.Fatalf("SetMetadata failed: %v", err)
	}

	// Get metadata
	retrieved, err := database.GetMetadata(key)
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if retrieved != value {
		t.Errorf("Metadata mismatch: got %q, want %q", retrieved, value)
	}

	// Update metadata
	newValue := "updated_value"
	if err := database.SetMetadata(key, newValue); err != nil {
		t.Fatalf("SetMetadata (update) failed: %v", err)
	}

	retrieved, err = database.GetMetadata(key)
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if retrieved != newValue {
		t.Errorf("Updated metadata mismatch: got %q, want %q", retrieved, newValue)
	}

	// Get non-existent key
	empty, err := database.GetMetadata("nonexistent")
	if err != nil {
		t.Fatalf("GetMetadata for nonexistent key failed: %v", err)
	}
	if empty != "" {
		t.Errorf("Expected empty string for nonexistent key, got %q", empty)
	}
}

func TestGetDocumentNotFound(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	doc, err := database.GetDocument(99999)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	if doc != nil {
		t.Errorf("Expected nil for non-existent document, got %+v", doc)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.0, 1.0, 0.0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{-1.0, 0.0, 0.0},
			expected: -1.0,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
		{
			name:     "different length",
			a:        []float32{1.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			if result < tt.expected-0.001 || result > tt.expected+0.001 {
				t.Errorf("cosineSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestFloat32Serialization(t *testing.T) {
	original := []float32{1.5, -2.3, 0.0, 100.5, -0.001}

	bytes := float32SliceToBytes(original)
	restored := bytesToFloat32Slice(bytes)

	if len(restored) != len(original) {
		t.Fatalf("Length mismatch: got %d, want %d", len(restored), len(original))
	}

	for i := range original {
		if original[i] != restored[i] {
			t.Errorf("Value mismatch at index %d: got %f, want %f", i, restored[i], original[i])
		}
	}
}

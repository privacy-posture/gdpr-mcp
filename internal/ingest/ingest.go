package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jc/gdpr-mcp/internal/db"
)

// Config holds ingestion configuration
type Config struct {
	ChunkSize    int
	ChunkOverlap int
	UseOpenAI    bool
	OpenAIKey    string
	OpenAIModel  string
}

// DefaultConfig returns default ingestion configuration
func DefaultConfig() Config {
	return Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		UseOpenAI:    false,
		OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:  "text-embedding-3-small",
	}
}

// Ingester handles document ingestion
type Ingester struct {
	db     *db.DB
	config Config
}

// New creates a new Ingester
func New(database *db.DB, config Config) *Ingester {
	return &Ingester{
		db:     database,
		config: config,
	}
}

// IngestFile ingests a text file into the database
func (ing *Ingester) IngestFile(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	return ing.IngestText(string(content))
}

// IngestText ingests text content into the database
func (ing *Ingester) IngestText(content string) error {
	// Split into chunks
	chunks := ing.chunkText(content)

	fmt.Printf("Ingesting %d chunks...\n", len(chunks))

	for i, chunk := range chunks {
		// Insert chunk
		docID, err := ing.db.InsertChunk(chunk, i)
		if err != nil {
			return fmt.Errorf("failed to insert chunk %d: %w", i, err)
		}

		// Generate and insert trigrams
		trigrams := db.GenerateTrigrams(chunk)
		if err := ing.db.InsertTrigrams(docID, trigrams); err != nil {
			return fmt.Errorf("failed to insert trigrams for chunk %d: %w", i, err)
		}

		// Generate and insert embedding
		embedding, err := ing.generateEmbedding(chunk)
		if err != nil {
			fmt.Printf("Warning: failed to generate embedding for chunk %d: %v\n", i, err)
			// Use stub embedding if real embedding fails
			embedding = stubEmbedding(chunk)
		}

		if err := ing.db.InsertEmbedding(docID, embedding); err != nil {
			return fmt.Errorf("failed to insert embedding for chunk %d: %w", i, err)
		}

		if (i+1)%10 == 0 {
			fmt.Printf("Processed %d/%d chunks\n", i+1, len(chunks))
		}
	}

	// Store metadata
	if err := ing.db.SetMetadata("ingested_at", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("failed to set metadata: %w", err)
	}
	if err := ing.db.SetMetadata("chunk_count", fmt.Sprintf("%d", len(chunks))); err != nil {
		return fmt.Errorf("failed to set metadata: %w", err)
	}

	fmt.Printf("Successfully ingested %d chunks\n", len(chunks))
	return nil
}

// chunkText splits text into overlapping chunks
func (ing *Ingester) chunkText(text string) []string {
	// Normalize whitespace
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")

	var chunks []string
	runes := []rune(text)
	textLen := len(runes)

	if textLen == 0 {
		return chunks
	}

	chunkSize := ing.config.ChunkSize
	overlap := ing.config.ChunkOverlap

	for start := 0; start < textLen; {
		end := start + chunkSize
		if end > textLen {
			end = textLen
		}

		// Try to break at sentence or word boundary
		if end < textLen {
			// Look for sentence boundary
			for i := end; i > start+chunkSize/2; i-- {
				if runes[i] == '.' || runes[i] == '\n' {
					end = i + 1
					break
				}
			}
			// Fall back to word boundary
			if end == start+chunkSize && end < textLen {
				for i := end; i > start+chunkSize/2; i-- {
					if runes[i] == ' ' {
						end = i
						break
					}
				}
			}
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}

		// Move start position with overlap
		start = end - overlap
		if start < 0 {
			start = 0
		}
		// Prevent infinite loop
		if end >= textLen {
			break
		}
	}

	return chunks
}

// generateEmbedding generates an embedding for the text
func (ing *Ingester) generateEmbedding(text string) ([]float32, error) {
	if ing.config.UseOpenAI && ing.config.OpenAIKey != "" {
		return openAIEmbedding(text, ing.config.OpenAIKey, ing.config.OpenAIModel)
	}
	return stubEmbedding(text), nil
}

// openAIEmbedding calls OpenAI embeddings API
func openAIEmbedding(text, apiKey, model string) ([]float32, error) {
	reqBody := map[string]interface{}{
		"input": text,
		"model": model,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embedding in response")
	}

	// Convert float64 to float32
	embedding := make([]float32, len(result.Data[0].Embedding))
	for i, v := range result.Data[0].Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// stubEmbedding generates a simple hash-based embedding for offline use
// This is NOT a real semantic embedding - just for testing/demo purposes
func stubEmbedding(text string) []float32 {
	const embeddingDim = 384 // Common embedding dimension

	embedding := make([]float32, embeddingDim)

	// Simple character-based hashing to create pseudo-embedding
	text = strings.ToLower(text)
	for i, r := range text {
		idx := (int(r) + i) % embeddingDim
		embedding[idx] += float32(r) / 1000.0
	}

	// Normalize
	var norm float32
	for _, v := range embedding {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(1.0 / float64(norm))
		for i := range embedding {
			embedding[i] *= norm
		}
	}

	return embedding
}

// EmbedQuery generates an embedding for a search query
func EmbedQuery(query string, useOpenAI bool, apiKey, model string) ([]float32, error) {
	if useOpenAI && apiKey != "" {
		return openAIEmbedding(query, apiKey, model)
	}
	return stubEmbedding(query), nil
}

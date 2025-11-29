package db

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	_ "embed"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// DB wraps the SQLite database connection
type DB struct {
	conn *sql.DB
}

// Document represents a text chunk
type Document struct {
	ID         int64
	Chunk      string
	ChunkIndex int
}

// SearchResult represents a search result with score
type SearchResult struct {
	ID      int64   `json:"id"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// Open opens or creates the database at the given path
func Open(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// Migrate applies the schema to the database
func (db *DB) Migrate() error {
	_, err := db.conn.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("failed to apply schema: %w", err)
	}
	return nil
}

// InsertChunk inserts a document chunk and returns its ID
func (db *DB) InsertChunk(chunk string, chunkIndex int) (int64, error) {
	result, err := db.conn.Exec(
		"INSERT INTO documents (chunk, chunk_index) VALUES (?, ?)",
		chunk, chunkIndex,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert chunk: %w", err)
	}
	return result.LastInsertId()
}

// InsertTrigrams inserts trigrams for a document
func (db *DB) InsertTrigrams(docID int64, trigrams []string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO trigrams (trigram, doc_id) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, trigram := range trigrams {
		if _, err := stmt.Exec(trigram, docID); err != nil {
			return fmt.Errorf("failed to insert trigram: %w", err)
		}
	}

	return tx.Commit()
}

// InsertEmbedding inserts a vector embedding for a document
func (db *DB) InsertEmbedding(docID int64, embedding []float32) error {
	blob := float32SliceToBytes(embedding)
	_, err := db.conn.Exec(
		"INSERT OR REPLACE INTO embeddings (doc_id, embedding) VALUES (?, ?)",
		docID, blob,
	)
	if err != nil {
		return fmt.Errorf("failed to insert embedding: %w", err)
	}
	return nil
}

// GetDocument retrieves a document by ID
func (db *DB) GetDocument(id int64) (*Document, error) {
	row := db.conn.QueryRow(
		"SELECT id, chunk, chunk_index FROM documents WHERE id = ?",
		id,
	)

	var doc Document
	err := row.Scan(&doc.ID, &doc.Chunk, &doc.ChunkIndex)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get document: %w", err)
	}
	return &doc, nil
}

// SearchTrigrams searches documents by trigram similarity
func (db *DB) SearchTrigrams(query string, limit int) ([]SearchResult, error) {
	queryTrigrams := GenerateTrigrams(strings.ToLower(query))
	if len(queryTrigrams) == 0 {
		return nil, nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(queryTrigrams))
	args := make([]interface{}, len(queryTrigrams))
	for i, t := range queryTrigrams {
		placeholders[i] = "?"
		args[i] = t
	}

	// Count matching trigrams per document
	sqlQuery := fmt.Sprintf(`
		SELECT d.id, d.chunk, COUNT(DISTINCT t.trigram) as match_count
		FROM documents d
		JOIN trigrams t ON d.id = t.doc_id
		WHERE t.trigram IN (%s)
		GROUP BY d.id
		ORDER BY match_count DESC
		LIMIT ?
	`, strings.Join(placeholders, ","))

	args = append(args, limit)

	rows, err := db.conn.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search trigrams: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	queryTrigramCount := float64(len(queryTrigrams))

	for rows.Next() {
		var id int64
		var chunk string
		var matchCount int
		if err := rows.Scan(&id, &chunk, &matchCount); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Calculate Jaccard-like similarity score
		score := float64(matchCount) / queryTrigramCount

		// Create snippet (first 200 chars)
		snippet := chunk
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}

		results = append(results, SearchResult{
			ID:      id,
			Score:   score,
			Snippet: snippet,
		})
	}

	return results, rows.Err()
}

// SearchVectors searches documents by vector similarity
func (db *DB) SearchVectors(queryEmbedding []float32, limit int) ([]SearchResult, error) {
	rows, err := db.conn.Query(`
		SELECT e.doc_id, e.embedding, d.chunk
		FROM embeddings e
		JOIN documents d ON e.doc_id = d.id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query embeddings: %w", err)
	}
	defer rows.Close()

	type scored struct {
		id      int64
		score   float64
		snippet string
	}

	var scoredDocs []scored

	for rows.Next() {
		var docID int64
		var embeddingBlob []byte
		var chunk string
		if err := rows.Scan(&docID, &embeddingBlob, &chunk); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		embedding := bytesToFloat32Slice(embeddingBlob)
		similarity := cosineSimilarity(queryEmbedding, embedding)

		snippet := chunk
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}

		scoredDocs = append(scoredDocs, scored{
			id:      docID,
			score:   similarity,
			snippet: snippet,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending
	sort.Slice(scoredDocs, func(i, j int) bool {
		return scoredDocs[i].score > scoredDocs[j].score
	})

	// Limit results
	if len(scoredDocs) > limit {
		scoredDocs = scoredDocs[:limit]
	}

	results := make([]SearchResult, len(scoredDocs))
	for i, s := range scoredDocs {
		results[i] = SearchResult{
			ID:      s.id,
			Score:   s.score,
			Snippet: s.snippet,
		}
	}

	return results, nil
}

// HybridSearch performs a combined trigram and vector search
func (db *DB) HybridSearch(query string, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	// Get trigram results
	trigramResults, err := db.SearchTrigrams(query, limit*2)
	if err != nil {
		return nil, err
	}

	// If no embedding provided, return trigram results only
	if queryEmbedding == nil {
		if len(trigramResults) > limit {
			trigramResults = trigramResults[:limit]
		}
		return trigramResults, nil
	}

	// Get vector results
	vectorResults, err := db.SearchVectors(queryEmbedding, limit*2)
	if err != nil {
		return nil, err
	}

	// Merge results using reciprocal rank fusion
	scores := make(map[int64]float64)
	snippets := make(map[int64]string)

	const k = 60.0 // RRF constant

	for i, r := range trigramResults {
		scores[r.ID] += 1.0 / (k + float64(i+1))
		snippets[r.ID] = r.Snippet
	}

	for i, r := range vectorResults {
		scores[r.ID] += 1.0 / (k + float64(i+1))
		if _, exists := snippets[r.ID]; !exists {
			snippets[r.ID] = r.Snippet
		}
	}

	// Convert to sorted results
	type scoredDoc struct {
		id    int64
		score float64
	}
	var sorted []scoredDoc
	for id, score := range scores {
		sorted = append(sorted, scoredDoc{id, score})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	results := make([]SearchResult, len(sorted))
	for i, s := range sorted {
		results[i] = SearchResult{
			ID:      s.id,
			Score:   s.score,
			Snippet: snippets[s.id],
		}
	}

	return results, nil
}

// SetMetadata sets a metadata key-value pair
func (db *DB) SetMetadata(key, value string) error {
	_, err := db.conn.Exec(
		"INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)",
		key, value,
	)
	return err
}

// GetMetadata retrieves a metadata value by key
func (db *DB) GetMetadata(key string) (string, error) {
	var value string
	err := db.conn.QueryRow(
		"SELECT value FROM metadata WHERE key = ?",
		key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// GenerateTrigrams generates trigrams from a string
func GenerateTrigrams(s string) []string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)

	if len(s) < 3 {
		return nil
	}

	seen := make(map[string]bool)
	var trigrams []string

	runes := []rune(s)
	for i := 0; i <= len(runes)-3; i++ {
		t := string(runes[i : i+3])
		if !seen[t] {
			seen[t] = true
			trigrams = append(trigrams, t)
		}
	}

	return trigrams
}

// Helper functions for embedding serialization

func float32SliceToBytes(floats []float32) []byte {
	bytes := make([]byte, len(floats)*4)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(bytes[i*4:], math.Float32bits(f))
	}
	return bytes
}

func bytesToFloat32Slice(bytes []byte) []float32 {
	floats := make([]float32, len(bytes)/4)
	for i := range floats {
		bits := binary.LittleEndian.Uint32(bytes[i*4:])
		floats[i] = math.Float32frombits(bits)
	}
	return floats
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

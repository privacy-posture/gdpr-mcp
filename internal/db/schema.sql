-- GDPR MCP Server Schema
-- SQLite database schema for storing GDPR document chunks with trigram and vector search

-- Main document chunks table
CREATE TABLE IF NOT EXISTS documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chunk TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Trigram index for text search
CREATE TABLE IF NOT EXISTS trigrams (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trigram TEXT NOT NULL,
    doc_id INTEGER NOT NULL,
    FOREIGN KEY (doc_id) REFERENCES documents(id) ON DELETE CASCADE
);

-- Index for faster trigram lookups
CREATE INDEX IF NOT EXISTS idx_trigrams_trigram ON trigrams(trigram);
CREATE INDEX IF NOT EXISTS idx_trigrams_doc_id ON trigrams(doc_id);

-- Vector embeddings table (stores as JSON float array or blob)
CREATE TABLE IF NOT EXISTS embeddings (
    doc_id INTEGER PRIMARY KEY,
    embedding BLOB NOT NULL,
    FOREIGN KEY (doc_id) REFERENCES documents(id) ON DELETE CASCADE
);

-- Metadata table for tracking ingestion state
CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

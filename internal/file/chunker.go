// Package file handles file chunking, hashing, and metadata.
//
// STATUS: Stub — implemented in Block 7 (after QUIC)
//
// DESIGN:
//
//   WHY CHUNK FILES?
//   A 1GB file can't be sent in one packet.
//   Chunking enables:
//   1. Parallel transfer: request chunk 0-9 from different peers simultaneously
//   2. Resume: if transfer interrupted, request only missing chunks
//   3. Verification: each chunk has its own hash — detect corruption early
//   4. Deduplication: two files sharing chunks (e.g. similar videos) only
//      need one copy of each shared chunk
//
//   CHUNK SIZE: 256KB is a good default.
//   Smaller = more overhead (more packets, more hashes).
//   Larger = less parallelism, more wasted work on retransmit.
//
//   NAMING:
//   File info:  /file/<fileHash>/info
//   Chunk:      /file/<fileHash>/chunk/<idx>
//   fileHash = SHA256(entire file) — content-addressed
//
//   FILE INFO PACKET (content of /file/<fileHash>/info):
//   {
//     "name":        "thesis.pdf",
//     "size_bytes":  10485760,
//     "chunk_size":  262144,
//     "chunk_count": 40,
//     "file_hash":   "a3f2...",
//     "chunk_hashes": ["b1c2...", "d3e4...", ...],  ← merkle-like
//     "mime_type":   "application/pdf",
//     "created_at":  "2026-02-27T10:00:00Z"
//   }
//
//   TRANSFER FLOW:
//   1. Requester: Interest /file/<hash>/info
//   2. Producer: Data with info packet
//   3. Requester: verify chunk_count, plan parallel requests
//   4. Requester: Interest /file/<hash>/chunk/0 through /chunk/N-1 (parallel)
//   5. Each chunk: verify SHA256(chunk) == chunk_hashes[i]
//   6. All chunks received: reassemble, verify SHA256(full) == fileHash
//
//   MERKLE VERIFICATION:
//   chunk_hashes[i] = SHA256(chunk_i_bytes)
//   fileHash = SHA256(concat(chunk_hashes))
//   This lets us verify each chunk independently as it arrives.
package file

import "github.com/H-strangeone/lan-suite/internal/config"

// ChunkSize is the default chunk size in bytes.
const ChunkSize = 256 * 1024 // 256KB

// Chunker splits files into CCN-named chunks.
type Chunker struct {
	cfg *config.Config
}

// NewChunker creates a Chunker.
func NewChunker(cfg *config.Config) *Chunker {
	return &Chunker{cfg: cfg}
}
// Package storage provides disk persistence for the LAN Suite node.
// It abstracts over the underlying storage format so we can swap
// implementations (flat files, bbolt, sqlite) without changing callers.
//
// STATUS: Stub — implemented in Block 5 (chat persistence)
//
// DESIGN:
//
//   Storage layout on disk (cfg.DataDir):
//   data/
//   ├── node.key          ← Ed25519 private key (0600)
//   ├── node.pub          ← Ed25519 public key  (0644)
//   ├── ccn/              ← persisted CCN content store
//   │   └── <namehash>    ← one file per Data packet
//   ├── chat/             ← chat message history
//   │   └── <roomID>/
//   │       └── <seqno>   ← one file per message
//   └── drive/            ← local drive files
//       └── <nodeID>/
//           └── <path>    ← mirrored file tree
//
//   WHY FLAT FILES instead of SQLite?
//   - No dependency, no CGO
//   - Each Data packet maps naturally to one file
//   - OS filesystem already handles concurrent access correctly
//   - Easy to inspect, backup, and debug
//   For indexing/querying we add bbolt (pure Go B-tree) in Block 5.
package storage

import (
	"os"

	"github.com/H-strangeone/lan-suite/internal/config"
)

// Store manages disk persistence.
type Store struct {
	cfg *config.Config
}

// New creates a Store and ensures all required directories exist.
func New(cfg *config.Config) (*Store, error) {
	dirs := []string{
		cfg.DataDir,
		cfg.DataDir + "/ccn",
		cfg.DataDir + "/chat",
		cfg.DataDir + "/drive",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, err
		}
	}
	return &Store{cfg: cfg}, nil
}
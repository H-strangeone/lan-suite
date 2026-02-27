// Package file (transfer.go) manages concurrent chunk fetching and serving.
//
// STATUS: Stub — implemented in Block 7 alongside chunker.go
//
// DESIGN:
//
//   FETCHING (downloading from peers):
//   type Fetcher struct {
//     router   *ccn.Router
//     quic     *transport.QUICServer
//     inflight map[string]*fetchJob  // fileHash → job
//   }
//
//   fetchJob tracks one file download:
//   - Which chunks we have
//   - Which chunks are in-flight
//   - Which peers have which chunks (from peer announcements)
//   - Retry logic for failed chunks
//
//   SERVING (uploading to peers):
//   When an Interest arrives for /file/<hash>/chunk/<n>:
//   1. Check CS — if cached, serve immediately
//   2. Read chunk from disk storage
//   3. Build Data packet, sign, return
//
//   PROGRESS TRACKING:
//   Emit events: {type:"progress", file:hash, pct:42}
//   Frontend receives via WebSocket → shows progress bar
package file

import "github.com/H-strangeone/lan-suite/internal/config"

// Transfer manages file fetch and serve operations.
type Transfer struct {
	cfg *config.Config
}

// NewTransfer creates a Transfer manager.
func NewTransfer(cfg *config.Config) *Transfer {
	return &Transfer{cfg: cfg}
}
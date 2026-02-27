// Package drive implements the distributed LAN drive.
// A shared namespace where any peer can read files that others have shared.
//
// STATUS: Stub — implemented in Block 8 (after file transfer)
//
// DESIGN:
//
//   The drive is a virtual filesystem with CCN-named content.
//   /drive/<nodeID>/<path>   files owned by a node
//   /drive/<nodeID>/index    directory listing for a node
//
//   SHARING a file:
//   1. Chunker splits file, stores chunks in CS and on disk
//   2. Drive updates its index: /drive/<nodeID>/index
//   3. Discovery announces: "I have /drive/<nodeID>/*"
//   4. Other nodes add FIB route: /drive/<nodeID> → this node
//
//   BROWSING another node's drive:
//   1. Interest /drive/alice/index → receive directory listing
//   2. User selects a file
//   3. Interest /drive/alice/documents/thesis.pdf/info → metadata
//   4. Begin parallel chunk fetch
//
//   SYNC (optional, Phase 5):
//   Two nodes can sync a folder by comparing indexes and fetching missing files.
//   This is how a "shared folder" feature works.
//
//   CONFLICT RESOLUTION:
//   Last-writer-wins based on CreatedAt timestamp in Data packet.
//   For collaborative editing: vector clocks (advanced, Phase 5+).
//
//   OFFLINE ACCESS:
//   Files cached in CS are available even when the source node is offline.
//   Drive index is also cached — you can browse offline if you visited recently.
package drive

import "github.com/H-strangeone/lan-suite/internal/config"

// Drive manages the distributed file namespace.
type Drive struct {
	cfg *config.Config
}

// New creates a Drive.
func New(cfg *config.Config) *Drive {
	return &Drive{cfg: cfg}
}
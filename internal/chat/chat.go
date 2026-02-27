// Package chat implements CCN-based peer-to-peer chat.
// Messages are content items with names — stored, cached, and replayed.
//
// STATUS: Stub — implemented in Block 5 (after discovery)
//
// DESIGN:
//
//   Message naming: /chat/<roomID>/messages/<seqno>
//   Room members:   /chat/<roomID>/members
//
//   SENDING a message:
//   1. Assign seqno = room.nextSeqno()
//   2. Build Data packet: name=/chat/room1/messages/42, content=<json>
//   3. Sign with node keypair
//   4. router.ProduceData(data) → stores in CS, satisfies pending interests
//   5. Announce via discovery: "I have /chat/room1/messages/42"
//
//   RECEIVING messages (online):
//   Interest arrives → router checks CS → found → serve
//
//   RECEIVING messages (offline catchup):
//   Node reconnects → queries /chat/room1/messages/<lastSeen+1>
//   Peers who have it respond → node catches up sequentially
//
//   PERSISTENCE:
//   Messages stored in internal/storage/ (built in Block 5)
//   On startup, load message history into CS from disk
//
// MESSAGE FORMAT (content field of Data packet):
//   {
//     "id":         "uuid",
//     "room":       "room1",
//     "from_node":  "aa:bb:cc:dd:ee:ff",
//     "from_name":  "Alice",
//     "text":       "Hello!",
//     "timestamp":  "2026-02-27T10:00:00Z",
//     "reply_to":   "uuid or null"
//   }
package chat

import "github.com/H-strangeone/lan-suite/internal/config"

// Manager handles chat rooms and message routing.
type Manager struct {
	cfg *config.Config
}

// New creates a chat Manager.
func New(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}